import AppKit
URLSession.shared.dataTask(with: u).resume()
let s = [SIGTERM, SIGINT, SIGHUP].map { sig in
  signal(sig, SIG_IGN)
  let source = DispatchSource.makeSignalSource(signal: sig, queue: .main)
  source.resume()
  return source
}
