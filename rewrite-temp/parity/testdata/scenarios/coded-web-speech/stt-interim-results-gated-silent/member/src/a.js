r.interimResults = true;
r.onresult = (e) => { if (e.results[0].isFinal) go(); };
