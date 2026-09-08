package relay

import "github.com/QuantumNous/new-api/relay/channel/openai"

// CleanupStaleResponsesStreamPreCommitFiles removes abandoned private spill
// files before the process starts serving relay traffic.
func CleanupStaleResponsesStreamPreCommitFiles() {
	openai.CleanupStaleResponsesStreamPreCommitFiles()
}
