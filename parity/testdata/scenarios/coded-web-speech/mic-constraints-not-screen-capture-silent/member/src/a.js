const tab = { audio: { suppressLocalAudioPlayback: true } };
navigator.mediaDevices.getDisplayMedia(tab);
navigator.mediaDevices.getUserMedia({ audio: true });
// getUserMedia({ restrictOwnAudio: true })
