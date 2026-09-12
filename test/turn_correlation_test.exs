defmodule SymphonyElixir.TurnCorrelationTest do
  use SymphonyElixir.TestSupport

  test "reviewer and stale terminal events do not finish the root turn or suppress tools and rate limits" do
    for foreign <- [
          terminal("turn/completed", "review-thread", "review-turn"),
          terminal("turn/completed", "root-thread", "stale-turn"),
          terminal("turn/failed", "review-thread", "review-turn", "failed"),
          terminal("turn/cancelled", "review-thread", "review-turn", "interrupted"),
          %{"method" => "turn/completed"}
        ] do
      result = replay(foreign, terminal("turn/completed", "root-thread", "root-turn"))
      assert {:ok, %{thread_id: "root-thread", turn_id: "root-turn"}} = result
      assert_receive :review_tool_called
      assert_receive %{event: :notification, payload: %{"method" => "account/rateLimits/updated"}}
      assert_receive %{event: :notification, payload: %{"method" => "item/completed"}}
      assert_receive %{event: :turn_completed, payload: %{"params" => %{"threadId" => "root-thread", "turn" => %{"id" => "root-turn"}}}}
      refute_receive %{event: :turn_completed}, 0
    end
  end

  test "matching current-protocol failed and interrupted turns retain their failure outcome" do
    for {status, expected} <- [{"failed", :turn_failed}, {"interrupted", :turn_cancelled}] do
      assert {:error, {^expected, _}} = replay(nil, terminal("turn/completed", "root-thread", "root-turn", status))
      assert_receive :review_tool_called
    end
  end

  defp replay(foreign, final) do
    root = Path.dirname(Workflow.workflow_file_path())
    script = Path.join(root, "correlation-provider.py")

    File.write!(script, """
    import json,sys
    foreign=json.loads(sys.argv[1]); final=json.loads(sys.argv[2])
    def send(message): print(json.dumps(message),flush=True)
    for line in sys.stdin:
        message=json.loads(line)
        if message.get("method")=="turn/start":
            send({"id":message["id"],"result":{"turn":{"id":"root-turn"}}})
            if foreign: send(foreign)
            send({"id":99,"method":"item/tool/call","params":{"threadId":"review-thread","turnId":"review-turn","tool":"review","arguments":{}}})
        elif message.get("id")==99:
            assert message["result"]["success"]
            send({"method":"account/rateLimits/updated","params":{"rateLimits":{}}})
            send({"method":"item/completed","params":{"threadId":"root-thread","turnId":"root-turn","item":{"type":"subAgentActivity","kind":"completed"}}})
            send(final)
    """)

    port =
      Port.open({:spawn_executable, System.find_executable("python3")}, [
        :binary,
        :exit_status,
        {:line, 100_000},
        {:args, [script, Jason.encode!(foreign), Jason.encode!(final)]}
      ])

    session = %{
      port: port,
      metadata: %{},
      approval_policy: "never",
      auto_approve_requests: true,
      turn_sandbox_policy: %{"type" => "dangerFullAccess"},
      thread_id: "root-thread",
      workspace: root,
      dynamic_tool_binding: %{}
    }

    test_pid = self()

    try do
      AppServer.run_turn(session, "Correlation regression", %{id: "test", identifier: "TEST-1", title: "Correlation"},
        on_message: &send(test_pid, &1),
        tool_executor: fn "review", %{} ->
          send(test_pid, :review_tool_called)
          %{"success" => true, "output" => "Reviewed"}
        end
      )
    after
      AppServer.stop_session(session)
    end
  end

  defp terminal(method, thread, turn, status \\ "completed") do
    %{"method" => method, "params" => %{"threadId" => thread, "turn" => %{"id" => turn, "status" => status, "items" => []}}}
  end
end
