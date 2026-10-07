function k(n) {
  switch (n) {
    case 'not-allowed': return 'denied';
    case 'no-speech': return 'quiet';
    default: return 'other';
  }
}
function j(n) { switch (n) { case 'network': return 1; case 'aborted': return 2; }
  return 'other';
}
switch (e) { case 'network': retry(); break; case 'aborted': stop(); break; }
