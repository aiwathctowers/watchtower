import Foundation
@testable import WatchtowerDesktop

/// Shared test double for `CLIRunnerProtocol`. Accumulates every invocation
/// so assertions can cover sequences of calls, not just the latest.
final class FakeCLIRunner: CLIRunnerProtocol {
    private let stdoutData: Data
    var shouldThrow: Error?
    private(set) var invocations: [[String]] = []

    init(stdout: Data = Data(), error: Error? = nil) {
        self.stdoutData = stdout
        self.shouldThrow = error
    }

    func run(args: [String]) async throws -> Data {
        invocations.append(args)
        if let shouldThrow { throw shouldThrow }
        return stdoutData
    }
}
