if (r.interimResults == true) {}
const a = { interimResults: undefined };
r.interimResults = opts.interim;
r.interimResults = true;
r.addEventListener('result', async function (e) { deliver(e); });
