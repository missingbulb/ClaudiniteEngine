// getUserMedia
export async function w() {
  const s = await navigator.mediaDevices.getUserMedia({ audio: true });
  s.getTracks();
}
