package workitem

import "testing"

func TestOnlyTheQueueVocabularyIsApproved(t *testing.T) {
	for _, l := range []string{StatusBlocked, StatusReady, StatusDone, StatusRejected, StatusNeedsHumanFailure, Urgent, OriginPlanned, OriginManual, OriginAdHoc} {
		if !Approved(l) {
			t.Errorf("%s is the queue's own label and is approved", l)
		}
	}
	for _, l := range []string{"workflow-failure", "claudinite-update", "task:origin:github", InReviewLabel, QueuedLabel, NeedsHuman, LegacyReady, OutcomeDelivered, ""} {
		if Approved(l) {
			t.Errorf("%q is not on the approved list", l)
		}
	}
	for _, l := range QueueLabels {
		if !Approved(l.Name) {
			t.Errorf("the queue ensures %q, which is not approved", l.Name)
		}
	}
}
