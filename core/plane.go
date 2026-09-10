package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

func (a Installation) boardInitialized() (bool, error) {
	client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/instances/", a.Settings.Port))
	if err != nil {
		return false, errors.New("could not check Plane setup; run switchyard logs plane and rerun init")
	}
	defer response.Body.Close()
	var state struct {
		Instance *struct {
			IsSetupDone *bool `json:"is_setup_done"`
		} `json:"instance"`
	}
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("Plane setup check failed (HTTP %d); rerun init after the board is ready", response.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&state); err != nil || state.Instance == nil || state.Instance.IsSetupDone == nil {
		return false, errors.New("Plane returned an invalid setup status; existing accounts are preserved")
	}
	return *state.Instance.IsSetupDone, nil
}

func (a Installation) startBoard() error {
	if err := a.compose("up", "-d"); err != nil {
		return err
	}
	if err := waitHTTP(fmt.Sprintf("http://127.0.0.1:%d/api/instances/", a.Settings.Port), 5*time.Minute); err != nil {
		return fmt.Errorf("board is not ready; run switchyard logs plane, fix the cause and rerun init or up: %w", err)
	}
	return nil
}

func waitHTTP(url string, timeout time.Duration) error {
	client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	deadline := time.Now().Add(timeout)
	for {
		response, err := client.Get(url)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", url)
		}
		time.Sleep(time.Second)
	}
}
