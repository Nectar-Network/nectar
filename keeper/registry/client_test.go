package registry

import "testing"

// Structured matching on the real KeeperRegistry error codes
// (AlreadyRegistered=3, NotRegistered=4), with the variant-name fallback when a
// numeric code is not present in the message.
func TestErrorClassification(t *testing.T) {
	t.Run("by code", func(t *testing.T) {
		if !isAlreadyRegistered("HostError: Error(Contract, #3)") {
			t.Error("#3 should classify as AlreadyRegistered")
		}
		if !isNotRegistered("Error(Contract, #4)") {
			t.Error("#4 should classify as NotRegistered")
		}
		if isAlreadyRegistered("Error(Contract, #4)") {
			t.Error("#4 (NotRegistered) must not match AlreadyRegistered")
		}
		if isNotRegistered("Error(Contract, #3)") {
			t.Error("#3 (AlreadyRegistered) must not match NotRegistered")
		}
		if isAlreadyRegistered("Error(Contract, #7)") || isNotRegistered("Error(Contract, #7)") {
			t.Error("#7 should match neither classifier")
		}
	})

	t.Run("name fallback", func(t *testing.T) {
		if !isAlreadyRegistered("AlreadyRegistered") {
			t.Error("name fallback for AlreadyRegistered")
		}
		if !isNotRegistered("get_keeper: KeeperNotRegistered") {
			t.Error("name fallback for NotRegistered")
		}
	})
}
