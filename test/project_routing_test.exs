defmodule SymphonyElixir.ProjectRoutingTest do
  use SymphonyElixir.TestSupport

  setup do
    root = Path.dirname(Workflow.workflow_file_path())
    previous_root = System.get_env("SWITCHYARD_ROOT")
    previous_repo = System.get_env("SOURCE_REPO_URL")
    System.put_env("SWITCHYARD_ROOT", Path.expand("../../.."))
    System.put_env("SOURCE_REPO_URL", "/wrong/global/repository")

    write_workflow_file!(Workflow.workflow_file_path(),
      tracker_kind: "memory",
      workspace_root: Path.join(root, "tasks"),
      hook_after_create: ~s("$SWITCHYARD_ROOT/scripts/task-workspace" init),
      hook_before_run: ~s("$SWITCHYARD_ROOT/scripts/task-workspace" check)
    )

    on_exit(fn ->
      restore_env("SWITCHYARD_ROOT", previous_root)
      restore_env("SOURCE_REPO_URL", previous_repo)
    end)

    %{root: root}
  end

  test "two project tasks clone, resume and push only to their mapped repositories", %{root: root} do
    for prefix <- ["ALPHA", "BETA"] do
      repo = repository(root, prefix)
      issue = issue(prefix, repo)
      assert {:ok, workspace} = Workspace.create_for_issue(issue)
      assert File.read!(Path.join(workspace, "project.txt")) == prefix
      assert git(workspace, ["remote", "get-url", "origin"]) == repo
      assert :ok = Workspace.run_before_run_hook(workspace, issue)
      assert git(workspace, ["config", "--local", "--get", "switchyard.issue-id"]) == issue.id
      assert git(workspace, ["config", "--local", "--get", "switchyard.project-id"]) == issue.native_ref["project_id"]
      File.write!(Path.join(workspace, "result.txt"), issue.id)
      git(workspace, ["add", "result.txt"])
      commit(workspace, "Finish task")
      git(workspace, ["push", "origin", "HEAD"])
      assert git(repo, ["show", "agent/#{prefix}-1:result.txt"]) == issue.id
      other = if prefix == "ALPHA", do: "BETA", else: "ALPHA"
      assert {_output, 1} = System.cmd("git", ["show-ref", "--verify", "--quiet", "refs/heads/agent/#{other}-1"], cd: repo)
    end
  end

  test "moved or remapped tasks stop before continuation or terminal cleanup", %{root: root} do
    accepted = issue("ALPHA", repository(root, "ALPHA"))
    assert {:ok, workspace} = Workspace.create_for_issue(accepted)
    File.write!(Path.join(workspace, "uncommitted.txt"), "keep this work")
    edited = %{accepted | title: "Edited title"}
    assert {:continue, _} = AgentRunner.continue_with_issue_for_test(accepted, fn _ -> {:ok, [edited]} end)

    for refreshed <- [
          %{accepted | native_ref: Map.put(accepted.native_ref, "repo_url", "https://example.invalid/other.git")},
          %{accepted | native_ref: Map.put(accepted.native_ref, "project_id", "another-project"), state: "Done"}
        ] do
      assert {:done, ^refreshed} = AgentRunner.continue_with_issue_for_test(accepted, fn _ -> {:ok, [refreshed]} end)

      pid =
        spawn(fn ->
          receive do
            :stop -> :ok
          end
        end)

      state = %Orchestrator.State{
        running: %{accepted.id => %{pid: pid, ref: nil, identifier: accepted.identifier, issue: accepted, started_at: DateTime.utc_now()}},
        claimed: MapSet.new([accepted.id]),
        codex_totals: %{input_tokens: 0, output_tokens: 0, total_tokens: 0, seconds_running: 0}
      }

      updated = Orchestrator.reconcile_issue_states_for_test([refreshed], state)
      refute Process.alive?(pid)
      refute Map.has_key?(updated.running, accepted.id)
      assert File.read!(Path.join(workspace, "uncommitted.txt")) == "keep this work"
    end
  end

  test "recorded and startup cleanup preserve every mismatched or unmarked checkout", %{root: root} do
    accepted = issue("ALPHA", repository(root, "ALPHA"))
    assert {:ok, workspace} = Workspace.create_for_issue(accepted)
    File.write!(Path.join(workspace, "uncommitted.txt"), "keep this work")

    for changed <- [
          %{accepted | id: "another-task"},
          %{accepted | native_ref: Map.put(accepted.native_ref, "project_id", "another-project")},
          %{accepted | native_ref: Map.put(accepted.native_ref, "repo_url", "https://example.invalid/other.git")}
        ] do
      assert {:error, _} = Workspace.run_before_run_hook(workspace, changed)
      assert {:error, :workspace_identity_unverified, ""} = Workspace.remove_recorded(workspace, nil, changed)
      assert :ok = Workspace.remove_issue_workspaces(changed)
      assert File.read!(Path.join(workspace, "uncommitted.txt")) == "keep this work"
    end

    git(workspace, ["checkout", "-b", "human-work"])
    assert {:error, :workspace_identity_unverified, ""} = Workspace.remove_recorded(workspace, nil, accepted)
    git(workspace, ["checkout", "agent/ALPHA-1"])
    git(workspace, ["config", "--local", "--unset", "switchyard.issue-id"])
    assert {:error, _} = Workspace.run_before_run_hook(workspace, accepted)
    assert :ok = Workspace.remove_issue_workspaces(accepted)
    assert File.exists?(workspace)
    git(workspace, ["config", "--local", "switchyard.issue-id", accepted.id])
    assert {:ok, _} = Workspace.remove_recorded(workspace, nil, accepted)
    refute File.exists?(workspace)
  end

  test "non-repository provider metadata can change without changing execution target" do
    issue = %Issue{id: "task", identifier: "TASK-1", native_ref: %{"section_gid" => "todo"}}
    assert Issue.same_work_item?(issue, %{issue | native_ref: %{"section_gid" => "in-progress"}})
    refute Issue.same_work_item?(issue, %{issue | native_ref: %{"project_id" => "project", "repo_url" => "https://example.invalid/repo.git"}})
  end

  test "legacy project hooks resume an existing unstamped checkout", %{root: root} do
    repo = repository(root, "LEGACY")
    System.put_env("SOURCE_REPO_URL", repo)
    legacy = %{issue("LEGACY", repo) | native_ref: %{"project_id" => "LEGACY-project"}}
    workspace = Path.join([root, "tasks", legacy.identifier])
    File.mkdir_p!(Path.dirname(workspace))
    git(root, ["clone", repo, workspace])
    git(workspace, ["checkout", "-b", "agent/LEGACY-1"])
    File.write!(Path.join(workspace, "uncommitted.txt"), "legacy work")

    assert {:ok, ^workspace} = Workspace.create_for_issue(legacy)
    assert :ok = Workspace.run_before_run_hook(workspace, legacy)
    assert {_output, 1} = System.cmd("git", ["config", "--local", "--get", "switchyard.issue-id"], cd: workspace)
    assert git(workspace, ["remote", "get-url", "origin"]) == repo
    assert File.read!(Path.join(workspace, "uncommitted.txt")) == "legacy work"

    # Adding a mapping must not silently adopt old work as a newly bound task.
    assert {:error, _} = Workspace.run_before_run_hook(workspace, issue("LEGACY", repo))
    assert {:error, :workspace_identity_unverified, ""} = Workspace.remove_recorded(workspace, nil, legacy)
    assert File.exists?(workspace)
  end

  test "SSH hooks preserve quoted repository paths and enforce cleanup identity", %{root: root} do
    bin = Path.join(root, "fake-ssh")
    File.mkdir_p!(bin)
    ssh = Path.join(bin, "ssh")
    File.write!(ssh, "#!/bin/sh\nfor last; do :; done\nexec sh -c \"$last\"\n")
    File.chmod!(ssh, 0o755)
    previous_path = System.get_env("PATH")
    System.put_env("PATH", bin <> ":" <> previous_path)
    on_exit(fn -> restore_env("PATH", previous_path) end)

    accepted = issue("REMOTE", repository(root, "REMOTE's"))
    assert {:ok, workspace} = Workspace.create_for_issue(accepted, "fixture")
    assert git(workspace, ["remote", "get-url", "origin"]) == accepted.native_ref["repo_url"]
    assert :ok = Workspace.run_before_run_hook(workspace, accepted, "fixture")
    File.write!(Path.join(workspace, "uncommitted.txt"), "remote work")
    changed = %{accepted | native_ref: Map.put(accepted.native_ref, "project_id", "other-project")}
    assert {:error, _} = Workspace.run_before_run_hook(workspace, changed, "fixture")
    assert {:error, :workspace_identity_unverified, ""} = Workspace.remove_recorded(workspace, "fixture", changed)
    assert File.read!(Path.join(workspace, "uncommitted.txt")) == "remote work"
    assert {:ok, _} = Workspace.remove_recorded(workspace, "fixture", accepted)
    refute File.exists?(workspace)
  end

  defp repository(root, name) do
    path = Path.join(root, name <> " source")
    File.mkdir_p!(path)
    git(path, ["init", "-q", "--initial-branch=main"])
    File.write!(Path.join(path, "project.txt"), name)
    git(path, ["add", "project.txt"])
    commit(path, "Initial")
    path
  end

  defp issue(prefix, repo) do
    %Issue{
      id: prefix <> "-uuid",
      identifier: prefix <> "-1",
      title: prefix,
      state: "In Progress",
      dispatchable: true,
      native_ref: %{"project_id" => prefix <> "-project", "repo_url" => repo}
    }
  end

  defp commit(path, message), do: git(path, ["-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", message])

  defp git(path, args) do
    {output, 0} = System.cmd("git", args, cd: path, stderr_to_stdout: true)
    String.trim(output)
  end
end
