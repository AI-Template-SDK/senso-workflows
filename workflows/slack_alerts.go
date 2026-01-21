package workflows

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

type SlackPayload struct {
	Text string `json:"text"`
}

// ReportErrorToSlack posts an error message to the eval-pipeline-alerts channel.
func ReportErrorToSlack(err error) error {
	if err == nil {
		return nil
	}

	webhookURL := os.Getenv("SLACK_WEBHOOK_URL")
	if webhookURL == "" {
		return errors.New("SLACK_WEBHOOK_URL environment variable is not set")
	}

	message := fmt.Sprintf(
		":rotating_light: *Eval Pipeline Error*\n"+
			"*Time:* %s\n"+
			"*Error:* ```%s```",
		time.Now().UTC().Format(time.RFC3339),
		err.Error(),
	)

	payload := SlackPayload{
		Text: message,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// ReportPipelineFailureToSlack reports pipeline failures with context.
func ReportPipelineFailureToSlack(pipeline, orgID, orgName, reason string, err error) error {
	if err == nil {
		return nil
	}

	if orgName == "" {
		orgName = "unknown"
	}
	if pipeline == "" {
		pipeline = "unknown"
	}
	if reason == "" {
		reason = "unknown"
	}

	reportErr := fmt.Errorf(
		"pipeline failed: pipeline=%s reason=%s org_id=%s org_name=%s error=%v",
		pipeline,
		reason,
		orgID,
		orgName,
		err,
	)

	return ReportErrorToSlack(reportErr)
}

// ReportNetworkFailureToSlack reports network pipeline failures with context.
func ReportNetworkFailureToSlack(pipeline, networkID, networkName, reason string, err error) error {
	if err == nil {
		return nil
	}

	if networkName == "" {
		networkName = "unknown"
	}
	if pipeline == "" {
		pipeline = "unknown"
	}
	if reason == "" {
		reason = "unknown"
	}

	reportErr := fmt.Errorf(
		"pipeline failed: pipeline=%s reason=%s network_id=%s network_name=%s error=%v",
		pipeline,
		reason,
		networkID,
		networkName,
		err,
	)

	return ReportErrorToSlack(reportErr)
}
