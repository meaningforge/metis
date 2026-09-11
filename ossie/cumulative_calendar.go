package ossie

import "fmt"

// ValidateCumulativeCalendarModel resolves non-built-in rolling and grain-to-date
// units through the model-declared custom-calendar contract. Rolling windows
// require a dense ordinal mapping. Grain-to-date reset units require an explicit
// custom grain whose hierarchy can later be matched against the query grain.
func ValidateCumulativeCalendarModel(model *SemanticModel, spec CumulativeMetricSpec) error {
	switch spec.Window.Type {
	case "rolling":
		if validCumulativeWindowUnit(spec.Window.Unit) {
			return nil
		}
		calendar, grain, err := resolveCustomCumulativeUnit(model, spec, "rolling")
		if err != nil {
			return err
		}
		if spec.TimeDimension != calendar.BaseTime {
			return fmt.Errorf("custom-calendar rolling unit %q requires time_dimension %q", spec.Window.Unit, calendar.BaseTime)
		}
		if !grain.DenseMapping {
			return fmt.Errorf("custom-calendar rolling unit %q requires dense_mapping=true", spec.Window.Unit)
		}
		return nil
	case "grain_to_date":
		if validGrainToDateUnit(spec.Window.Unit) {
			return nil
		}
		calendar, _, err := resolveCustomCumulativeUnit(model, spec, "grain-to-date")
		if err != nil {
			return err
		}
		if spec.TimeDimension != calendar.BaseTime {
			return fmt.Errorf("custom-calendar grain-to-date unit %q requires time_dimension %q", spec.Window.Unit, calendar.BaseTime)
		}
		return nil
	default:
		return nil
	}
}

func resolveCustomCumulativeUnit(model *SemanticModel, spec CumulativeMetricSpec, kind string) (CustomCalendarSpec, CustomCalendarGrain, error) {
	calendar, ok, err := CustomCalendar(model)
	if err != nil {
		return CustomCalendarSpec{}, CustomCalendarGrain{}, fmt.Errorf("resolve custom-calendar %s unit %q: %w", kind, spec.Window.Unit, err)
	}
	if !ok {
		return CustomCalendarSpec{}, CustomCalendarGrain{}, fmt.Errorf("unsupported %s cumulative window unit %q", kind, spec.Window.Unit)
	}
	grain, ok := CustomCalendarGrainByName(calendar, spec.Window.Unit)
	if !ok {
		return CustomCalendarSpec{}, CustomCalendarGrain{}, fmt.Errorf("unsupported %s cumulative window unit %q", kind, spec.Window.Unit)
	}
	return calendar, grain, nil
}
