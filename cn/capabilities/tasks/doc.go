// Package tasks is the task runner: the scheduler run (tasks/schedule),
// the executor loop (tasks/execute), the queue over GitHub issues
// (tasks/queue), the routine session's commands (tasks/items), the landing
// lane (tasks/land), the continuation chain (tasks/recover), the signals
// and precondition evaluator (tasks/signals, tasks/precondition), the
// embedded Node runner a worker or a task-local preconditions.mjs runs
// through (tasks/runner), the port onto GitHub (tasks/world) and its adapter
// (integrations/github/ghport) and the in-memory repository the scenario tests run
// against (tasks/sim). The task contract is capabilities/tasks/taskspec, the work
// item's grammar capabilities/tasks/workitem, the merge policy capabilities/tasks/mergepolicy.
//
// The claims below are the mechanism's, carried over from the Node pack's
// docs/PRINCIPLES.md: each is "Y happens
// when Z" and names the Go tests that prove it. The guard in doc_test.go
// keeps this honest both ways: every citation names a test that exists,
// and every test of the two scenario suites (schedule's run_test.go,
// execute's loop_test.go) is cited by at least one claim.
//
// # Schedule
//
//   - A quiet repository files nothing and leaves the drain gate shut; a
//     task whose conditions hold files one ready, planned item and opens it,
//     and while that item is live a second run files nothing.
//     schedule.TestAQuietRepoFilesNothingAndTheGateStaysShut,
//     schedule.TestACommitFilesOneReadyItemAndASecondRunFilesNothing
//   - schedule:at-most-<cadence> holds when no run of the task started or
//     ended since the current UTC period opened; an item closed rejected
//     never ran, so it covers no period.
//     schedule.TestTheCadenceDeclinesASecondRunInTheSamePeriod,
//     schedule.TestADeclinedItemCoversNoPeriod,
//     precondition.TestADeclinedRunCoversNoPeriod,
//     calendar.TestPeriodStart, calendar.TestPeriod
//   - The scheduler files an item only when the task's precondition holds:
//     it asks a task-local term through the runner as the executor does,
//     after every term the engine judges alone, so a task whose cadence
//     declines starts no runner. A term that holds files its context.
//     schedule.TestATaskLocalTermThatDeclinesFilesNothing,
//     schedule.TestATaskLocalTermThatHoldsFilesTheItemWithItsContext,
//     schedule.TestAScheduleDeclineRunsNoLocalTerm,
//     precondition.TestABuiltinDeclineAsksNoLocalTerm,
//     precondition.TestAPartialPassLeavesALocalTermUnknown
//   - A task whose signals cannot be read fails open into an item the
//     executor decides, the body and the run's log saying why.
//     schedule.TestAnUnreadableSignalFailsOpen
//   - A task whose own terms cannot be asked files nothing; the run asks
//     every other task, then fails, naming each such task and its error,
//     so the workflow's failure report files its issue.
//     schedule.TestTermsThatCannotBeAskedFileNothingAndFailTheRun
//   - A task over the fleet signal fails open on Node's sentence when the
//     run holds no FLEET_GITHUB_TOKEN, and asks the reader when it does.
//     schedule.TestAFleetTaskWithoutTheTokenFailsOpenOnNodesSentence,
//     schedule.TestAFleetTaskWithTheTokenAsksTheReader
//   - A second live unqualified item of one task is closed obsolete, the
//     oldest kept. schedule.TestADuplicateStandingItemSelfHeals
//   - Readiness has one site, the scheduler run: a blocked item is readied
//     only when its wait is over, and a close elsewhere leaves its dependent
//     blocked. schedule.TestABlockedItemIsReadiedOnlyWhenItsWaitIsOver,
//     execute.TestACloseLeavesItsDependentBlocked
//   - Forcing a scheduled task mints its missing standing item, stamped
//     Woken, and the wake reports what matched nothing.
//     schedule.TestAWakeMintsTheMissingStandingItemAndReportsWhatMatchedNothing
//   - The engine's own update is filed at most once a UTC day, at the
//     engine's path, and the fleet's bare force id reaches it; it closes on
//     its verdicts. schedule.TestTheEnginesUpdateIsFiledOnceADay,
//     execute.TestTheEnginesUpdateRunsAndClosesOnItsVerdicts
//   - The engine's own usage fold is filed on every repo the queue runs
//     in, at the engine's path, while its run mark stands before the UTC
//     day opened, as well as on a commit or a capture; the mark is read
//     from the rolling file, or the legacy one not yet moved.
//     schedule.TestTheUsageFoldIsFiledWhileTheMachineryRanUnfolded,
//     precondition.TestRunsSinceFoldReadsTheFilesOwnWatermark,
//     signals.TestReadLocalReadsTheUsageFoldsRunMark
//   - The engine's update hands its agent stage the engine PR it opened,
//     amending that PR's branch, only when that PR carries staged workflow
//     files. execute.TestTheEnginesUpdateHandsItsOwnPRToTheAgentStage
//   - A schedule_after dependent yields while its scheduled upstream is live.
//     execute.TestADependentYieldsWhileItsScheduledUpstreamIsLive
//
// # Execute
//
//   - The claim lease arbitrates by comment id within the item's current
//     episode; a losing claimant strikes its own claim, leaves the item and
//     drains the rest. execute.TestTheEarliestClaimWinsByCommentID,
//     execute.TestTheArbiterIsEpisodeScoped,
//     execute.TestALosingClaimantStrikesItsOwnClaim,
//     execute.TestALosingClaimantLeavesTheItemAndDrainsTheRest
//   - The pick-time skips are advisory: a won claim re-verifies them and
//     reverts on a conflict, and a reverted item is never re-picked by the
//     run that reverted it. execute.TestATwinHoldingAnEarlierClaimForcesARevert,
//     execute.TestAnUpstreamThatClaimedEarlierForcesItsDependentBack,
//     execute.TestAnItemThisRunRevertedIsNeverRePickedByIt
//   - A human re-queue is a label edit any executor may claim from at once.
//     execute.TestASecondExecutorWinsAParkedItemAHumanReQueued
//   - A hand-off closes the executor's episode, so an item requeued from
//     its agent is claimed afresh, while the agent-held item keeps its
//     claim's standing against a later twin.
//     execute.TestAnItemRequeuedFromItsAgentIsPickedAfresh,
//     execute.TestAnAgentHeldTwinStillHoldsItsEarlierClaim
//   - An executor run drains every pickable item, one at a time; a hand-off
//     ends its occupancy and the run keeps draining.
//     execute.TestARunDrainsEveryPickableItemOneAtATime,
//     execute.TestAHandOffEndsOccupancyAndTheRunKeepsDraining
//   - The executor re-evaluates the preconditions at pick: a decline closes
//     a scheduled item with the reason, a gate that declines a marked issue
//     rejects and closes it, and a precondition that could not answer parks
//     open. execute.TestADeclineClosesAScheduledItemWithTheReason,
//     execute.TestAMarkedIssueTheGateDeclinedIsRejectedAndClosed,
//     execute.TestAPreconditionThatCouldNotAnswerParksOpen
//   - The target is resolved once, from expected_outcome, and handed to both
//     phases; an unreadable pull request list parks the run and nothing
//     runs. execute.TestCodeWorkIsHandedTheResolvedTarget,
//     execute.TestTheHandOffStampsTheTargetOnTheItem,
//     execute.TestAnUnresolvableTargetParksAndNothingRuns,
//     execute.TestThePlannersMatrix, execute.TestATargetBecomesExactlyThreeVariables
//   - A heartbeat rides the work step, so a live run is never reclaimed, and
//     the leash reclaims a silent one and draws the episode boundary; a run
//     reclaimed mid-work leaves the item to its new holder.
//     execute.TestALongWorkStepLeavesHeartbeatsOnItsOwnItem,
//     schedule.TestAHeartbeatKeepsALongRunAlive,
//     schedule.TestTheLeashReclaimsASilentClaimAndDrawsTheEpisodeBoundary,
//     execute.TestARunReclaimedMidWorkLeavesTheItemToItsNewHolder
//   - A run that failed parks at failure carrying the worker's verdict, and
//     never hands off. execute.TestAFailedCodeWorkParksAtFailureCarryingTheWorkersVerdict,
//     execute.TestFailedCodeWorkNeverHandsOff
//   - A requeue re-arms Not-before, returns the item to blocked and strikes
//     the claim; on a marked issue it stamps the machine block; an
//     unreadable instant parks at failure.
//     execute.TestARequeueReArmsNotBeforeReturnsToBlockedAndStrikesTheClaim,
//     execute.TestARequeueOnAMarkedIssueStampsTheMachineBlock,
//     execute.TestARequeueWithAnUnreadableInstantParksAtFailure
//   - A declared secret the job does not carry parks the item at action,
//     naming it, and nothing runs; a carried one reaches the worker and no
//     other does. execute.TestAnUnconfiguredDeclaredSecretParksAtActionNamingIt,
//     execute.TestAnUnsetDeclaredSecretIsNamedAndNothingRuns,
//     execute.TestADeclaredSecretReachesTheWorkerAndNoOtherDoes
//   - An item whose task the checkout no longer carries closes obsolete,
//     including one deleted mid-run and one at a pre-rename path; a path
//     naming a different task or a malformed item goes to a human; an item
//     pointing elsewhere is refused.
//     execute.TestAnItemWhoseTaskIsGoneClosesObsolete,
//     execute.TestATaskDeletedFromTheCheckoutMidRunClosesObsolete,
//     execute.TestAnItemAtItsPreRenamePathClosesObsolete,
//     execute.TestAPathNamingADifferentTaskGoesToAHuman,
//     execute.TestAMalformedItemGoesToAHuman,
//     execute.TestAnItemPointingElsewhereIsRefused
//   - The ordinary path: agentless code-work closes done, with what
//     in-process code-work said; a pull request the lane merged closes done;
//     code-work that delivered no open pull request still closes. execute.TestAgentlessCodeWorkClosesDone,
//     execute.TestADeliveredPRTheLaneMergedClosesDone,
//     execute.TestCodeWorkThatDeliveredNoOpenPRStillCloses,
//     execute.TestWhatCodeWorkSaidClosesTheItem
//   - A marked issue closes like any other done item and keeps its origin.
//     execute.TestAMarkedIssueClosesLikeAnyOtherDoneItemAndKeepsItsOrigin
//
// # Session
//
//   - Invocation is one routine fire per item, ever: the hand-off swaps to
//     running-agent and fires exactly once; a refused fire parks; an
//     unanswered one leaves the item with the agent leash.
//     execute.TestAHandOffSwapsToRunningAgentAndInvokesExactlyOnce,
//     execute.TestARefusedInvocationParks,
//     execute.TestAnUnansweredInvocationLeavesTheItemWithTheAgent,
//     execute.TestAFireWithNoAnswerIsUnknownAndNeverRetried
//   - The session checks, never claims: it refuses an item it does not hold
//     and names the check that failed, and a request it implements must be
//     open and still marked. items.TestAnItemThisSessionDoesNotHoldIsRefused,
//     items.TestTheSessionGateNamesTheCheckThatFailed,
//     items.TestARequestItImplementsMustBeOpenAndStillMarked
//   - Code-work's artifacts reach the agent through the item, never twice.
//     execute.TestASupersedeCodeWorkPerformedIsNotHandedToTheAgentAgain,
//     execute.TestAHandedOffItemCarriesTheRecordOnItsHandOff
//
// # Deliver
//
//   - expected_outcome is a ceiling: a merge beyond it parks for a decision,
//     and code-work that opened a pull request it may not land parks for
//     approval. execute.TestAMergeBeyondTheCeilingParksForADecision,
//     execute.TestCodeWorkThatOpenedAPRParksForApprovalWithNoRecord
//   - A supersede run closes its incumbents once its own pull request
//     exists, and leaves them when it delivered nothing; a landed incumbent
//     ends the occurrence without running the work.
//     execute.TestASupersedeRunClosesItsIncumbentsOnceItsOwnExists,
//     execute.TestASupersedeRunThatDeliveredNothingLeavesItsIncumbents,
//     execute.TestALandedIncumbentEndsTheOccurrenceWithoutRunningTheWork
//   - One landing lane serves the update and the tasks: it merges at the
//     pinned sha when no pull_request CI exists, leaves a review member's
//     pull request after starting its checks, and never arms a doomed
//     auto-merge. land.TestDeliverMergesAtThePinnedShaWhenNoPRCIExists,
//     land.TestDeliverLeavesAReviewMembersPRAfterStartingItsChecks,
//     land.TestDeliverSkipsTheDoomedArmOnAnUngatedBaseAndLandsOnItsOwnEvidence
//   - The landing lane judges the pull request's own diff against the
//     task's automerge before it starts, arms or merges anything; a diff
//     outside it parks the item at action with the policy's reason, and a
//     green incumbent outside it never lands.
//     land.TestPinnedLandsOnlyADiffThePolicyAuthorizes,
//     land.TestDeliverJudgesBeforeArmingOrMerging,
//     execute.TestADeliveryOutsideThePolicyParksForAction,
//     execute.TestResolveLandsAGreenIncumbentOnlyInsideThePolicy
//
// # Recover
//
//   - A run that dies mid-queue dispatches the next link of its chain, and
//     past the bound the chain stops on the one failure issue.
//     recover.TestADeathWithinTheBoundDispatchesTheNextLink,
//     recover.TestPastTheBoundTheChainStopsOnTheOneFailureIssue
//   - A failure park nobody answered past its bound closes, and a torn item
//     settled before the write is left alone.
//     schedule.TestAnAbandonedFailureParkCloses,
//     schedule.TestATornItemSettledBeforeTheWriteIsLeftAlone
//   - A parked person's own issue whose task is gone stays open: the
//     queue's labels and machine block come off and nothing else changes,
//     while a filed item for a gone task closes rejected.
//     schedule.TestAPersonsOwnIssueWhoseTaskIsGoneIsReleasedNotClosed
//   - A parked person's own issue whose awaited pull request closed
//     without merging is released the same way and stays open, while one
//     whose pull request merged closes done and a filed item closes
//     rejected. schedule.TestAPersonsOwnIssueWhosePRClosedUnmergedIsReleasedNotClosed
//
// # Requests
//
//   - A marked issue is adopted once, as itself, and a stranger's
//     parameters are ignored. schedule.TestAMarkedIssueIsAdoptedOnceAsItself,
//     schedule.TestAStrangersParametersAreIgnored
//   - A marked issue naming no task is adopted into the one active task
//     gated on request-eligible, and waits while none or several are.
//     schedule.TestAMarkedIssueNamingNoTaskWaitsUnlessExactlyOneTaskTakesRequests
//
// # Cost
//
//   - Every settled item carries the run's cost record, growing across one
//     run; an approval park carries it without an execution record; a run
//     with no stopwatch writes none.
//     execute.TestTheRunStampsItsCostOnTheItemItSettled,
//     execute.TestTwoSettledItemsCarryGrowingSnapshotsOfOneRun,
//     execute.TestAnApprovalParkCarriesTheCostRecordWithoutAnExecRecord,
//     execute.TestARunWithNoStopwatchWritesNoCostRecord
//
// # Contract
//
//   - The status is one label, swapped with every legacy spelling removed;
//     a torn swap leaves the item without one, which recovery reads.
//     queue.TestSwapStatusRemovesEveryLegacySpelling,
//     queue.TestATornSwapLeavesTheItemWithoutAStatus
//   - A worker runs in its task folder with exactly the contract's
//     variables. execute.TestTheCodeWorkVariablesAreExactlyTheContractsNames,
//     execute.TestAShellWorkerRunsInItsTaskFolderWithTheContractsVariables
package tasks
