import AVFoundation
final class H {
  func start() {
    engine.inputNode.installTap(onBus: 0) { _, _ in }
  }
}
