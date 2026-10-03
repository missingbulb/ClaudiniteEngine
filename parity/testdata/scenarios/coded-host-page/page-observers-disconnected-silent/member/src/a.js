const o = new MutationObserver(f);
o.observe(r);
export const stop = () => o.disconnect();
