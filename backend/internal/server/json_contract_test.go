package server

import (
	"encoding/json"
	"testing"
)

func requireJSONObject(t testing.TB, data []byte, keys ...string) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatalf("response is not a JSON object: %v\n%s", err, data)
	}
	requireJSONKeys(t, object, keys...)
	return object
}

func requireJSONKeys(t testing.TB, object map[string]json.RawMessage, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			t.Fatalf("JSON object is missing required key %q: %v", key, object)
		}
	}
}

func requireJSONObjectField(t testing.TB, object map[string]json.RawMessage, key string, keys ...string) map[string]json.RawMessage {
	t.Helper()
	raw, ok := object[key]
	if !ok {
		t.Fatalf("JSON object is missing required object %q: %v", key, object)
	}
	return requireJSONObject(t, raw, keys...)
}

func requireJSONArrayField(t testing.TB, object map[string]json.RawMessage, key string) []json.RawMessage {
	t.Helper()
	raw, ok := object[key]
	if !ok {
		t.Fatalf("JSON object is missing required array %q: %v", key, object)
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		t.Fatalf("JSON field %q is not an array: %v\n%s", key, err, raw)
	}
	return values
}

func requireJSONStringField(t testing.TB, object map[string]json.RawMessage, key string) string {
	t.Helper()
	raw, ok := object[key]
	if !ok {
		t.Fatalf("JSON object is missing required string %q: %v", key, object)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("JSON field %q is not a string: %v\n%s", key, err, raw)
	}
	return value
}

func requireJobSnapshotContract(t testing.TB, data []byte) map[string]json.RawMessage {
	t.Helper()
	job := requireJSONObject(t, data, "id", "kind", "state", "phase", "progress", "output", "startedAt")
	state := requireJSONStringField(t, job, "state")
	if state == "running" {
		return job
	}
	requireJSONKeys(t, job, "finishedAt")
	if state == "failed" {
		requireJSONKeys(t, job, "error")
		return job
	}
	if state != "succeeded" {
		t.Fatalf("job has unsupported state %q", state)
	}

	result := requireJSONObjectField(t, job, "result")
	switch requireJSONStringField(t, job, "kind") {
	case "proton-install":
		requireJSONKeys(t, result, "toolName", "destinationId", "sha256", "restartSteam")
	case "umip-apply":
		requireJSONKeys(t, result, "bootloader", "restartRequired")
	case "module-install":
		requireJSONKeys(t, result, "inspection", "identity", "kernelRelease", "moduleName", "modulePath", "vermagic", "noOp", "signingRequired")
	}
	return job
}
