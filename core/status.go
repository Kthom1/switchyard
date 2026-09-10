package core

import (
	"errors"
	"fmt"

	"github.com/Kthom1/switchyard/config"
)

func (a Installation) Status() error {
	boardErr := a.compose("ps", "--all")
	runnerErr := a.run("systemctl", "--user", "is-active", config.Name(a.Root)+".service")
	if boardErr != nil {
		return boardErr
	}
	if runnerErr != nil {
		return errors.New("runner is stopped or failed; run switchyard logs runner")
	}
	if err := waitHTTP(fmt.Sprintf("http://127.0.0.1:%d/api/instances/", a.Settings.Port), 0); err != nil {
		return errors.New("board is unavailable; run switchyard logs plane")
	}
	if err := waitHTTP(fmt.Sprintf("http://127.0.0.1:%d/api/v1/state", a.Settings.RunnerPort), 0); err != nil {
		return errors.New("runner dashboard is unavailable; run switchyard logs runner")
	}
	return nil
}
