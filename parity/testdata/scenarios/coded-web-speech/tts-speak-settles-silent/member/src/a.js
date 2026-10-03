chrome.tts.speak(t, { onEvent(e) { if (['end', 'interrupted', 'cancelled', 'error'].includes(e.type)) r(); } });
chrome.tts.speak(t, { onEvent: log });
const u = new SpeechSynthesisUtterance(t);
u.onend = r;
u.onerror = r;
