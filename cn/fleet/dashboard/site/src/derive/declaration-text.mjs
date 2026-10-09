// A `task.json`'s text as the declaration the page renders, with the defaults the
// engine's contract fills. THE DASHBOARD'S OWN COPY of those two steps: the page reads
// a member's declarations in a browser.
import { DEFAULT_AUTOMERGE, DEFAULT_AGENT_MODEL } from '../read/queue-vocabulary.mjs';

// `$schema` is the editor's pointer, not a field of the contract, and leaves here.
export function parseTaskDeclaration(text) {
  const decl = JSON.parse(text);
  if (decl !== null && typeof decl === 'object' && !Array.isArray(decl)) delete decl.$schema;
  return decl;
}

// Fill the absent fields in place and return the declaration: no agent, and land
// nothing unreviewed for a task that may open a pull request.
export function applyTaskDefaults(out) {
  if (out.agent_model === undefined) out.agent_model = DEFAULT_AGENT_MODEL;
  if (out.expected_outcome !== undefined && out.expected_outcome !== 'no_code_changes' && out.automerge === undefined) out.automerge = DEFAULT_AUTOMERGE;
  return out;
}
