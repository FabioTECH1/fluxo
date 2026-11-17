package executiondomain

import (
	"bytes"
	"encoding/json"
	"errors"
)

// ActivationFile is an exact file transition planned by the owning subsystem
// while its coordination gate and exclusive lifecycle admission are held.
// "absent" is explicit creation authority, never a missing backup fallback.
type ActivationFile struct {
	Path         string `json:"path"`
	Before       string `json:"before_sha256"`
	Data         []byte `json:"data,omitempty"`
	Image        bool   `json:"executing_image,omitempty"`
	Cron         bool   `json:"cron_trigger,omitempty"`
	Mode         uint32 `json:"mode"`
	CronMTime    int64  `json:"cron_mtime,omitempty"`
	Directory    bool   `json:"create_directory,omitempty"`
	Lock         bool   `json:"coordination_gate,omitempty"`
	Fence        []byte `json:"creation_fence,omitempty"`
	PublishFence bool   `json:"-"`
}

// MergeActivationJSON preserves unrecognized historic fields while replacing
// the known fields deliberately converted by activation. Activation never
// deletes entries or invocation/history keys.
func MergeActivationJSON(before []byte, after any) ([]byte, error) {
	var original, replacement any
	decoder := json.NewDecoder(bytes.NewReader(before))
	decoder.UseNumber()
	if err := decoder.Decode(&original); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(after)
	if err != nil {
		return nil, err
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&replacement); err != nil {
		return nil, err
	}
	if _, ok := original.(map[string]any); !ok {
		return nil, errors.New("activation source is not a journal object")
	}
	var merge func(any, any) any
	merge = func(old, next any) any {
		a, aok := old.(map[string]any)
		b, bok := next.(map[string]any)
		if !aok || !bok {
			return next
		}
		for key, value := range b {
			a[key] = merge(a[key], value)
		}
		return a
	}
	return json.Marshal(merge(original, replacement))
}

// firstExplicit is the prototype schema which first required a saved V1 ID.
// The next schema adds new identities. Older records can never acquire a new
// interpretation merely from the current host or selected adapter.
func ValidateAdapter(schema, firstExplicit int, id AdapterID) error {
	if schema >= firstExplicit && id == "" {
		return errors.New("versioned invocation is missing its execution adapter")
	}
	if id == PersistentV1 && schema <= firstExplicit {
		return errors.New("new execution adapter requires the next reader-fenced journal format")
	}
	_, err := InvocationAdapter(id)
	return err
}
