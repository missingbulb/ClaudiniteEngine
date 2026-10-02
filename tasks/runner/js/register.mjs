// Loaded with --import before the runner: registers the resolve hook that
// answers @claudinite/sdk with the copy beside this file.
import { register } from 'node:module';

register('./hooks.mjs', import.meta.url, { data: { sdk: new URL('./sdk/index.mjs', import.meta.url).href } });
