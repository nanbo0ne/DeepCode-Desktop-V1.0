package computeruse

import (
	"fmt"
	"math"
)

func validateActionTarget(observation Observation, action Action) error {
	if action.SessionID != "" && action.SessionID != observation.SessionID {
		return ErrWrongOwner
	}
	if action.DisplayID != "" && action.DisplayID != observation.DisplayID {
		return ErrStaleObservation
	}
	if action.Generation == 0 || action.Generation != observation.Generation {
		return ErrStaleObservation
	}
	for _, value := range []float64{action.X, action.Y, action.EndX, action.EndY} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return fmt.Errorf("coordinates must be within the observed crop")
		}
	}
	if action.TimeoutMS < 0 || action.TimeoutMS > 30000 || len(action.Text) > 16384 {
		return fmt.Errorf("action exceeds bounded input limits")
	}
	if action.ElementID != "" {
		found := false
		for _, element := range observation.Elements {
			if element.ID != action.ElementID {
				continue
			}
			found = true
			if element.Password {
				return ErrProtectedSurface
			}
			if !element.Enabled {
				return fmt.Errorf("target element is disabled")
			}
		}
		if !found {
			return ErrStaleObservation
		}
	}
	if action.WindowID != "" {
		found := false
		for _, window := range observation.Windows {
			if window.ID != action.WindowID {
				continue
			}
			found = true
			if window.HigherTrust {
				return ErrProtectedSurface
			}
		}
		if !found {
			return ErrStaleObservation
		}
	}
	return nil
}
