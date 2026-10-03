const s = await navigator.mediaDevices.getUserMedia({ audio: true });
try {} finally { s.getTracks().forEach((t) => t.stop()); }
