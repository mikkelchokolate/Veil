package api

type PromotedServiceReloader struct {
	applyRoot string
	run       func([]string) ServiceActionResult
}

func NewPromotedServiceReloader(applyRoot string, run func([]string) ServiceActionResult) PromotedServiceReloader {
	if run == nil {
		run = serviceActionRunner
	}
	return PromotedServiceReloader{applyRoot: applyRoot, run: run}
}

func (r PromotedServiceReloader) Reload(liveFiles []string) []ServiceActionResult {
	commands := NewPromotedServiceActionCatalog(r.applyRoot).Commands(liveFiles)
	results := make([]ServiceActionResult, 0, len(commands))
	for _, command := range commands {
		result := r.run(command)
		if result.Name == "" && len(command) > 0 {
			result.Name = command[len(command)-1]
		}
		if result.Command == nil {
			result.Command = append([]string(nil), command...)
		}
		results = append(results, result)
		if !result.Success {
			break
		}
	}
	return results
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
