package daemon

import (
	"encoding/json"
	"strings"

	"github.com/gongahkia/zsh-git-inlay/internal/gitstate"
	"github.com/gongahkia/zsh-git-inlay/internal/ipc"
)

// Diagnostic is an explicitly requested repository view. Interactive lookup
// remains silent and never waits for generation or diagnostics.
type Diagnostic struct {
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	Action      string `json:"action"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Provider    string `json:"provider,omitempty"`
}

func (server *Server) diagnose(cwd string) ipc.Reply {
	ctx, cancel := gitstate.WithTimeout()
	defer cancel()
	state, err := gitstate.Snapshot(ctx, cwd)
	diagnostic := Diagnostic{Status: string(state.Availability), Reason: state.Reason, Fingerprint: state.Fingerprint}
	if err != nil {
		diagnostic.Status = "snapshot_failed"
		diagnostic.Reason = err.Error()
	}
	if diagnostic.Status == "ready" {
		reply := server.lookup(ipc.Request{Version: ipc.Version, Operation: "lookup", Fingerprint: state.Fingerprint, Repository: state.RepoID, Worktree: state.WorktreeID})
		diagnostic.Status = reply.Status
		diagnostic.Reason = reply.Error
		if reply.Status == "ready" {
			var record Record
			if json.Unmarshal(reply.Payload, &record) == nil {
				diagnostic.Provider = record.Provider.Name
				if record.Policy.Body == "required" {
					diagnostic.Status = "body_required"
				}
			}
		} else if failure := server.failure(state.Fingerprint); failure != "" && (reply.Status == "pending" || reply.Status == "stale") {
			diagnostic.Status = "generation_failed"
			diagnostic.Reason = failure
			if strings.Contains(failure, "grounding") || strings.HasPrefix(failure, "rank candidates:") {
				diagnostic.Status = "withheld_by_policy"
			}
		} else if reply.Status == "pending" && !server.pending(state.Fingerprint) {
			diagnostic.Status = "not_prepared"
		}
	}
	diagnostic.Action = DiagnosticAction(diagnostic.Status)
	payload, _ := json.Marshal(diagnostic)
	return ipc.Reply{Version: ipc.Version, Status: "ready", Payload: payload}
}

func DiagnosticAction(status string) string {
	switch status {
	case "disabled", "snoozed":
		return "Run zsh-git-inlay control status --repo, then enable the disabled scope or wait for its snooze to expire."
	case "controls_invalid":
		return "Run zsh-git-inlay doctor to inspect the private controls file; restore its backup or correct the reported cause."
	case "ready":
		return "Type git commit -m followed by a space; accept with your usual autosuggestion binding."
	case "pending":
		return "Preparation is running; type another character or fetch suggestions again when it finishes."
	case "not_prepared", "stale", "activity_stale":
		return "Run zsh-git-inlay observe --cwd . to prepare the current staged state."
	case "no_staged_changes":
		return "Stage the changes you intend to commit."
	case "outside_repository":
		return "Change into a Git worktree."
	case "conflicted_index":
		return "Resolve and stage the conflicted files."
	case "body_required":
		return "Run zsh-git-inlay compose to prepare the required commit body."
	case "generation_failed":
		return "Check the selected provider, runtime and model; use doctor and review local generation budgets before retrying observe."
	case "withheld_by_policy":
		return "Review repository message policy and grounding evidence; use a factual subject or adjust the policy explicitly."
	case "constrained_no_match":
		return "Shorten the typed message prefix or cycle the prepared matching candidates."
	case "unsupported_command":
		return "Use a supported git commit -m form with the cursor at the end."
	case "daemon_unavailable":
		return "Return to a prompt or run zsh-git-inlay observe --cwd .; use doctor if startup fails."
	default:
		return "Run zsh-git-inlay doctor and check the global/repository configuration."
	}
}
