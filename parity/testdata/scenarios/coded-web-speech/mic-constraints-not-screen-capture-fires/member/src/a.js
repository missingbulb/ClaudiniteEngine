const c = {
  audio: { restrictOwnAudio: true, suppressLocalAudioPlayback: true },
};
navigator.mediaDevices.getUserMedia(c);
navigator.mediaDevices.getUserMedia(c);
navigator.mediaDevices.getUserMedia({ audio: { suppressLocalAudioPlayback: true } });
const o = make.opts(
  { suppressLocalAudioPlayback: false });
getUserMedia(o);
