import XCTest
@testable import AIAgent

/// AgentConfig.custom* と register* の整合 (Phase 2-1, B1〜B4)。
/// 主に「フィールドが存在する」「init で適切に保持される」レベルのユニットテスト。
/// 実バイナリと連動した検証は AgentE2ETests に追加する。
final class LifecycleTests: XCTestCase {

    func testCustomGuardsFieldPreservedThroughInit() {
        let g = GuardSpec.input(name: "no_secrets") { _ in .allow }
        let cfg = AgentConfig(
            binary: "agent",
            guards: GuardsConfig(input: ["no_secrets"]),
            customGuards: [g]
        )
        XCTAssertEqual(cfg.customGuards?.count, 1)
        XCTAssertEqual(cfg.customGuards?.first?.name, "no_secrets")
        // GuardsConfig (有効化リスト) は別フィールドとして保持される
        XCTAssertEqual(cfg.guards?.input, ["no_secrets"])
    }

    func testCustomToolsFieldPreservedThroughInit() {
        let t = Tool(name: "echo") { _ in .text("ok") }
        let cfg = AgentConfig(
            binary: "agent",
            toolScope: ToolScopeConfig(maxTools: 2, includeAlways: ["echo"]),
            customTools: [t]
        )
        XCTAssertEqual(cfg.customTools?.count, 1)
        XCTAssertEqual(cfg.customTools?.first?.name, "echo")
        // toolScope.includeAlways で参照される名前と一致
        XCTAssertEqual(cfg.toolScope?.includeAlways, ["echo"])
    }

    func testCustomVerifiersFieldPreservedThroughInit() {
        let v = Verifier(name: "non_empty") { _, _, r in r.isEmpty ? .fail("e") : .pass }
        let cfg = AgentConfig(
            binary: "agent",
            verify: VerifyConfig(verifiers: ["non_empty"]),
            customVerifiers: [v]
        )
        XCTAssertEqual(cfg.customVerifiers?.count, 1)
        XCTAssertEqual(cfg.customVerifiers?.first?.name, "non_empty")
        XCTAssertEqual(cfg.verify?.verifiers, ["non_empty"])
    }

    func testCustomJudgesFieldPreservedThroughInit() {
        let judge: JudgeHandler = { resp, _ in
            resp.count > 30 ? .done("long enough") : .continue
        }
        let cfg = AgentConfig(
            binary: "agent",
            judge: JudgeConfig(name: "concise"),
            customJudges: ["concise": judge]
        )
        XCTAssertEqual(cfg.customJudges?.count, 1)
        XCTAssertNotNil(cfg.customJudges?["concise"])
        XCTAssertEqual(cfg.judge?.name, "concise")
    }

    func testAllCustomFieldsCanBeOmitted() {
        let cfg = AgentConfig(binary: "agent")
        XCTAssertNil(cfg.customTools)
        XCTAssertNil(cfg.customGuards)
        XCTAssertNil(cfg.customVerifiers)
        XCTAssertNil(cfg.customJudges)
    }

    func testCustomFieldsCoexistWithLegacyConfigBlocks() {
        // 旧 API 互換: customGuards に書かなくても、後で agent.registerGuards で
        // 追加できる経路は引き続き使える。両方の経路が共存可能であること。
        let cfg = AgentConfig(
            binary: "agent",
            guards: GuardsConfig(input: ["a", "b"]),
            // a だけ自動 register。b はテスト後に手動で register する想定。
            customGuards: [GuardSpec.input(name: "a") { _ in .allow }]
        )
        XCTAssertEqual(cfg.customGuards?.count, 1)
        XCTAssertEqual(cfg.guards?.input?.count, 2)
    }
}
