import AppKit
let s = [SIGTERM, SIGINT].map { sig in
  let source = DispatchSource.makeSignalSource(signal: sig, queue: .main)
  source.resume()
  signal(sig, SIG_IGN)
  return source
}
