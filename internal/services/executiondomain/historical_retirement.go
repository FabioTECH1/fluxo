package executiondomain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
)

const SchedulerHistoricalRetirementSupport = 7
const SchedulerHistoricalRetirementRole = "scheduler-historical-retirement"
const SchedulerHistoricalRetirementContract = "fenced-historical-retirement-v1"

// This admission is additional to the unchanged backend write guard. A core
// marker alone cannot supply the operation, source or reader-fence authority.
func GuardHistoricalRetirementWrite(dataDir, operation, sourceSHA string) error {
	floor, err := currentWriteFloor(dataDir)
	if err != nil {
		return err
	}
	if floor.ActivationID != operation {
		return errors.New("pending retirement is not the committed reader operation")
	}
	return guardHistoricalRetirementArchive(dataDir, *floor, operation, sourceSHA)
}

func GuardHistoricalRetirementArchive(dataDir, operation, sourceSHA string) error {
	floor, err := currentWriteFloor(dataDir)
	if err != nil {
		return err
	}
	return guardHistoricalRetirementArchive(dataDir, *floor, operation, sourceSHA)
}

func guardHistoricalRetirementArchive(dataDir string, floor ReaderFloor, operation, sourceSHA string) error {
	if floor.SchedulerSupport < 7 || !floorRoleContains(floor, SchedulerHistoricalRetirementRole, SchedulerHistoricalRetirementContract) ||
		!roleContains(Capabilities(), SchedulerHistoricalRetirementRole, SchedulerHistoricalRetirementContract) {
		return errors.New("historical retirement requires its exact reader contract")
	}
	seen := map[string]bool{}
	for depth := 0; depth < 64; depth++ {
		if seen[floor.ActivationID] {
			return errors.New("cyclic historical retirement archive chain")
		}
		seen[floor.ActivationID] = true
		raw, err := readProtected(filepath.Join(dataDir, "execution-activations", floor.ActivationID, "plan.json"), 128<<20)
		if err != nil {
			return err
		}
		if historicalDigest(raw) != floor.PlanSHA256 {
			return errors.New("historical retirement archive changed")
		}
		var plan struct {
			Format            int              `json:"format"`
			Kind              string           `json:"kind"`
			ID                string           `json:"id"`
			InstallationID    string           `json:"installation_id"`
			BinarySHA256      string           `json:"binary_sha256"`
			SourceFloor       []byte           `json:"source_floor"`
			SourceFloorSHA256 string           `json:"source_floor_sha256"`
			Files             []ActivationFile `json:"files"`
		}
		if err = json.Unmarshal(raw, &plan); err != nil {
			return err
		}
		if plan.ID != floor.ActivationID || plan.InstallationID != floor.InstallationID || plan.BinarySHA256 != floor.BinarySHA256 {
			return errors.New("historical archive identity changed")
		}
		if plan.ID == operation {
			if plan.Format != 4 || (plan.Kind != "historical-retirement" && plan.Kind != "installer-historical-scheduler-and-task-retirement") {
				return errors.New("archive is not a historical retirement operation")
			}
			for i, file := range plan.Files {
				if file.Path != "/var/lib/fluxo-scheduler/state.json" {
					continue
				}
				if file.Before != sourceSHA {
					return errors.New("historical source digest differs from archived original")
				}
				original, e := readProtected(filepath.Join(dataDir, "execution-activations", operation, fmt.Sprintf("before-%d", i)), 32<<20)
				if e != nil {
					return e
				}
				if historicalDigest(original) != sourceSHA {
					return errors.New("historical original core archive changed")
				}
				var target struct {
					Retirement struct {
						Contract   string `json:"contract"`
						Operation  string `json:"operation"`
						Source     string `json:"source_sha256"`
						Phase      string `json:"phase"`
						Completion string `json:"completion_sha256"`
					} `json:"historical_retirement"`
				}
				if e = json.Unmarshal(file.Data, &target); e != nil {
					return e
				}
				h := target.Retirement
				if h.Contract != SchedulerHistoricalRetirementContract || h.Operation != operation || h.Source != sourceSHA || h.Phase != "pending" || h.Completion != "" {
					return errors.New("historical archive lost its exact pending conversion")
				}
				return nil
			}
			return errors.New("historical archive lost its source core")
		}
		if len(plan.SourceFloor) == 0 || historicalDigest(plan.SourceFloor) != plan.SourceFloorSHA256 {
			return errors.New("current floor does not retain the historical operation")
		}
		next, e := ParseReaderFloor(plan.SourceFloor)
		if e != nil {
			return e
		}
		if next.Phase != "committed" || next.InstallationID != floor.InstallationID {
			return errors.New("historical archive lineage changed installation or phase")
		}
		floor = next
	}
	return errors.New("historical archive chain exceeds its bound")
}

func historicalDigest(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
