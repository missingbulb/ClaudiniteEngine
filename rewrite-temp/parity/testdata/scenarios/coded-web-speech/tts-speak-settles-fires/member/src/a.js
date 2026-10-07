chrome.tts.speak(t, {
  onEvent(e) { if (e.type === 'end' || e.type === "error") resolve(); },
});
const u = new SpeechSynthesisUtterance(t);
u.addEventListener('end', r);
