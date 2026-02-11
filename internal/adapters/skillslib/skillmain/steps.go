package skillmain

// StepDone is called when a skill step completes. Pass nil for success.
type StepDone func(err error)

// Step starts a named step within a skill execution.
// It logs the step start via the RunContext logger and returns a done
// function that logs completion with elapsed time.
//
// Usage:
//
//	done := skillmain.Step(rc, "fetch_data")
//	data, err := fetchData(ctx)
//	done(err)
//	if err != nil { return err }
func Step(rc *RunContext, name string) StepDone {
	start := rc.Now()
	rc.Logger.Debug().Str("step", name).Msg("step started")
	return func(err error) {
		end := rc.Now()
		ms := end.Sub(start).Milliseconds()
		lg := rc.Logger.Info().Str("step", name).Int64("duration_ms", ms)
		if err != nil {
			lg.Err(err).Msg("step failed")
		} else {
			lg.Msg("step done")
		}
	}
}
