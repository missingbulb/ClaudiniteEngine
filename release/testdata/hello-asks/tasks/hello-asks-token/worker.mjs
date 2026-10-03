import { log } from '@claudinite/sdk';

export async function worker(params) {
  log(`HELLO_TOKEN ${params.secrets.HELLO_TOKEN ? 'handed over' : 'missing'}`);
}
