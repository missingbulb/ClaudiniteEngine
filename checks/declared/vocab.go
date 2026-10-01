package declared

// The declaration vocabulary, copied from the Node engine's
// pattern-rules.mjs at the commit the parity harness pins; a drift test
// reads that file when CLAUDINITE_NODE_ENGINE names a checkout.

// specKeys names the keys each container of a declaration may hold.
var specKeys = map[string][]string{
	"spec":                         {"id", "on_fail", "severity", "since", "failureMessage", "fix", "scope", "scanFiles", "scanTracked", "excludeFiles", "scanFileClasses", "excludeFileClasses", "scanIgnoringComments", "scanIgnoringMarkdownFences", "relevantWhen", "whenMissing", "maxLines", "maxLineLength", "skipLinesMatching", "matchLines", "countMatchingLines", "checkEachFile", "repoWide", "requirePaths", "forbidTrackedPathsMatching", "extractValueSets", "requireIndexCoverage", "checkParsedFiles", "forbidReferences", "checkSetValues", "checkSetPairs", "requireIdenticalFiles", "checkBranchCommits", "forbidIntroducedMergeCommits", "forbidAddedValueInArray", "forbidAddedLinesMatching", "forbidRemovedLinesMatching", "requireCoChange", "flagUntrackedFilesMatching", "whenReplyClassIncludes", "guardToolCalls", "checkKeyValueFile", "checkSections"},
	"checkParsedFiles":             {"file", "filesMatching", "whereFileContains", "everyScannedFile", "forEachEntryAtField", "whereEntryFieldEquals", "whenFieldPresent", "requireField", "requireFieldMatching", "forbidField", "forbidValueInArray", "requireValueInArray", "requireEqualFields", "what", "fix"},
	"requireFieldMatching":         {"field", "pattern"},
	"scanFiles":                    {"inParsedFilesMatching", "whereFileContains", "namedByField", "defaultingTo", "withSuffix"},
	"whereEntryFieldEquals":        {"field", "equals"},
	"requireEqualFields":           {"field", "inFile", "atField", "whenFileMissing", "whenUnequal"},
	"whenFileMissing":              {"what", "fix"},
	"extractValueSets":             {"setName", "fromParsedFile", "fromParsedFilesMatching", "whereFileContains", "valuesOfArraysAtFields", "valuesAtFields", "fromLinesMatching", "inFilesMatching", "splitValuesOn", "fromTrackedPathsMatching", "fromAddedLinesMatching", "whenSetEmpty"},
	"whenSetEmpty":                 {"what", "fix"},
	"checkSetValues":               {"setName", "valueIsPattern", "requireSomeFileMatching", "forbidEveryFileMatching", "requirePathExists", "requireTrackedPathMatching", "what", "fix"},
	"requireSomeFileMatching":      {"pathMatching", "text"},
	"forbidEveryFileMatching":      {"pathMatching", "text"},
	"checkSetPairs":                {"everyValueOf", "mustAlsoBeIn", "mustNotBeIn", "what", "fix"},
	"requireIdenticalFiles":        {"everyFileMatching", "twinAt", "whenTwinAbsent", "what", "fix"},
	"whenTwinAbsent":               {"what", "fix"},
	"requireIndexCoverage":         {"eachTrackedPathMatching", "eachScannedPathMatching", "includeVendored", "whoseTextMatches", "eachValueOfSet", "indexFile", "coveredByText", "coveredByGlobLinesMatching", "coveredByValueInArrayAtField", "whenIndexFileAbsent", "anchorFindingsAt", "what", "fix"},
	"coveredByValueInArrayAtField": {"atField", "value", "ignoreCase", "matchingEntryObjectsByField"},
	"forbidReferences":             {"from", "to", "between", "siblings", "scope", "allow", "except", "matchNames", "alsoMatchNames", "matchUniqueFilenames", "reason"},
	"except":                       {"path", "to", "reason"},
	"relevantWhen":                 {"pathExists", "pathAbsent", "trackedFileMatches", "noTrackedFileMatches", "exactlyOneTrackedFileMatches", "someTrackedFileContains", "scanningWholeRepo", "repoContains"},
	"someTrackedFileContains":      {"pathMatching", "text", "ignoringComments"},
	"whenMissing":                  {"what", "fix"},
	"maxLines":                     {"limit", "what", "fix"},
	"maxLineLength":                {"bytes", "what", "fix"},
	"matchLines":                   {"match", "andLineMatches", "unlessLineMatches", "unlessPreviousLineMatches", "andIndentedBlockBelowMatches", "unlessIndentedBlockBelowMatches", "andWithinBlockOpenedBy", "unlessWithinBlockOpenedBy", "whenPathMatches", "whenFileMatches", "unlessFileMatches", "what", "fix"},
	"countMatchingLines":           {"linesMatching", "atLeast", "atMost", "what", "fix"},
	"checkEachFile":                {"relevantWhen", "whenFileMatches", "require", "forbid", "what", "fix"},
	"repoWide":                     {"unlessSomeFileMatches", "flagFilesMatching", "neverFlagFiles", "what", "fix"},
	"requirePaths":                 {"path", "what", "fix"},
	"forbidTrackedPathsMatching":   {"match", "what", "fix"},
	"checkBranchCommits":           {"someMessageMatches", "unlessOnDefaultBranch", "what", "fix"},
	"forbidIntroducedMergeCommits": {"what", "fix"},
	"forbidAddedValueInArray":      {"file", "filesMatching", "whereFileContains", "atFields", "what", "fix"},
	"forbidAddedLinesMatching":     {"inFilesMatching", "match", "unlessLineMatches", "what", "fix"},
	"forbidRemovedLinesMatching":   {"inFilesMatching", "match", "unlessLineMatches", "unlessMatchRemainsInFile", "what", "fix"},
	"requireCoChange":              {"whenChangedFileMatches", "whenAddedLineMatches", "requireChangedFileMatching", "what", "fix"},
	"whenAddedLineMatches":         {"inFilesMatching", "match"},
	"flagUntrackedFilesMatching":   {"match", "what", "fix"},
	"guardToolCalls":               {"tool", "inputField", "match", "requireMatch", "unlessMatches", "inputMatches", "unlessInputMatches", "inputFieldAbsent", "atMostPerSession", "what", "fix"},
	"whenUnequal":                  {"what", "fix"},
	"forbidValueInArray":           {"atField", "value", "ignoreCase", "matchingEntryObjectsByField"},
	"requireValueInArray":          {"atField", "value", "ignoreCase", "matchingEntryObjectsByField"},
	"checkKeyValueFile":            {"file", "keys", "whenMissing", "whenLineNotKeyValue", "whenKeyUnknown", "whenKeyMissing"},
	"whenLineNotKeyValue":          {"what", "fix"},
	"whenKeyUnknown":               {"what", "fix"},
	"whenKeyMissing":               {"what", "fix"},
	"checkSections":                {"section", "sections", "requirePresent", "requireFirstOnPage", "forbidProseLines", "eachBulletBlockMatches", "eachBulletLeadsWithDate", "minBullets", "maxBullets", "maxBulletBlockLength", "newestDatedBulletWithinDays"},
	"requirePresent":               {"what", "fix"},
	"requireFirstOnPage":           {"what", "fix"},
	"forbidProseLines":             {"what", "fix"},
	"eachBulletBlockMatches":       {"pattern", "what", "fix"},
	"eachBulletLeadsWithDate":      {"whenUndated", "whenNotRealDate"},
	"whenUndated":                  {"what", "fix"},
	"whenNotRealDate":              {"what", "fix"},
	"minBullets":                   {"count", "what", "fix"},
	"maxBullets":                   {"count", "what", "fix"},
	"maxBulletBlockLength":         {"characters", "what", "fix"},
	"newestDatedBulletWithinDays":  {"days", "what", "fix"},
}

// fileClassNames lists the file classes in the order an error names them.
var fileClassNames = []string{"javascriptFiles", "pythonFiles", "markdownFiles", "workflowFiles", "testFiles"}

// fileClassSources are the classes' patterns.
var fileClassSources = map[string]string{
	"javascriptFiles": `\.(mjs|cjs|jsx?|mts|cts|tsx?)$`,
	"pythonFiles":     `\.py$`,
	"markdownFiles":   `\.md$`,
	"workflowFiles":   `^\.github\/workflows\/[^/]+\.ya?ml$`,
	"testFiles":       `(^|\/)(tests?|__tests__|__mocks__|spec|fixtures?)\/|\.(test|spec)\.|_test\.[a-z]+$`,
}

var fileClasses = func() map[string]*Regex {
	out := map[string]*Regex{}
	for k, v := range fileClassSources {
		out[k] = mustRegex(v, "")
	}
	return out
}()

// pathOrPatternKeys take a /pattern/flags string or a plain path.
var pathOrPatternKeys = set("scanFiles", "excludeFiles", "tool")

// patternKeys take a /pattern/flags string.
var patternKeys = set(
	"skipLinesMatching", "match", "andLineMatches", "unlessLineMatches",
	"unlessPreviousLineMatches", "andIndentedBlockBelowMatches", "unlessIndentedBlockBelowMatches",
	"andWithinBlockOpenedBy", "unlessWithinBlockOpenedBy", "whenPathMatches", "whenFileMatches", "unlessFileMatches",
	"require", "forbid", "trackedFileMatches", "noTrackedFileMatches", "exactlyOneTrackedFileMatches",
	"pathMatching", "text", "repoContains", "unlessSomeFileMatches", "flagFilesMatching",
	"neverFlagFiles", "eachTrackedPathMatching", "eachPathMatching", "globLineMatching",
	"filesMatching", "whereFileContains", "inFilesMatching", "pattern", "linesMatching",
	"eachScannedPathMatching", "coveredByGlobLinesMatching", "whoseTextMatches",
	"fromParsedFilesMatching", "someMessageMatches", "inParsedFilesMatching",
	"fromLinesMatching", "fromTrackedPathsMatching", "fromAddedLinesMatching", "splitValuesOn",
	"everyFileMatching", "whenChangedFileMatches", "requireChangedFileMatching",
	"requireMatch", "unlessMatches", "inputMatches", "unlessInputMatches",
)

// templateContainers hold regex templates, filled per value before they
// compile.
var templateContainers = set("requireSomeFileMatching", "forbidEveryFileMatching")

// workAssertions read the change, so they need scope "work".
var workAssertions = []string{"checkBranchCommits", "forbidIntroducedMergeCommits", "forbidAddedValueInArray",
	"forbidAddedLinesMatching", "forbidRemovedLinesMatching", "requireCoChange", "flagUntrackedFilesMatching",
	"whenReplyClassIncludes"}

var replyClasses = []string{"correction", "feature", "process-change", "other"}

func set(keys ...string) map[string]bool {
	out := map[string]bool{}
	for _, k := range keys {
		out[k] = true
	}
	return out
}
