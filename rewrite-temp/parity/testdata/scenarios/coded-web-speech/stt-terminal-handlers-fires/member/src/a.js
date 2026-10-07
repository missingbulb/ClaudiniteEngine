const r = new webkitSpeechRecognition();
r.onresult = (e) => go(e);
