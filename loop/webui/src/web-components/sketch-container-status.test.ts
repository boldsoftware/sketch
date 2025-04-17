import { html, fixture, expect } from "@open-wc/testing";
import "./sketch-container-status";
import type { SketchContainerStatus } from "./sketch-container-status";
import { State } from "../types";

describe("SketchContainerStatus", () => {
  // Mock complete state for testing
  const mockCompleteState: State = {
    hostname: "test-host",
    working_dir: "/test/dir",
    initial_commit: "abcdef1234567890",
    message_count: 42,
    os: "linux",
    title: "Test Session",
    total_usage: {
      input_tokens: 1000,
      output_tokens: 2000,
      cache_read_input_tokens: 300,
      cache_creation_input_tokens: 400,
      total_cost_usd: 0.25
    }
  };

  it("renders with complete state data", async () => {
    const el: SketchContainerStatus = await fixture(html`
      <sketch-container-status .state=${mockCompleteState}></sketch-container-status>
    `);

    // Check that all expected elements exist
    expect(el.shadowRoot!.querySelector("#hostname")).to.exist;
    expect(el.shadowRoot!.querySelector("#workingDir")).to.exist;
    expect(el.shadowRoot!.querySelector("#initialCommit")).to.exist;
    expect(el.shadowRoot!.querySelector("#messageCount")).to.exist;
    expect(el.shadowRoot!.querySelector("#inputTokens")).to.exist;
    expect(el.shadowRoot!.querySelector("#outputTokens")).to.exist;
    expect(el.shadowRoot!.querySelector("#cacheReadInputTokens")).to.exist;
    expect(el.shadowRoot!.querySelector("#cacheCreationInputTokens")).to.exist;
    expect(el.shadowRoot!.querySelector("#totalCost")).to.exist;

    // Verify content of displayed elements
    expect(el.shadowRoot!.querySelector("#hostname")!.textContent).to.equal("test-host");
    expect(el.shadowRoot!.querySelector("#workingDir")!.textContent).to.equal("/test/dir");
    expect(el.shadowRoot!.querySelector("#initialCommit")!.textContent).to.equal("abcdef12"); // Only first 8 chars
    expect(el.shadowRoot!.querySelector("#messageCount")!.textContent).to.equal("42");
    expect(el.shadowRoot!.querySelector("#inputTokens")!.textContent).to.equal("1000");
    expect(el.shadowRoot!.querySelector("#outputTokens")!.textContent).to.equal("2000");
    expect(el.shadowRoot!.querySelector("#cacheReadInputTokens")!.textContent).to.equal("300");
    expect(el.shadowRoot!.querySelector("#cacheCreationInputTokens")!.textContent).to.equal("400");
    expect(el.shadowRoot!.querySelector("#totalCost")!.textContent).to.equal("$0.25");
  });

  it("renders with undefined state", async () => {
    const el: SketchContainerStatus = await fixture(html`
      <sketch-container-status></sketch-container-status>
    `);

    // Elements should exist but be empty
    expect(el.shadowRoot!.querySelector("#hostname")!.textContent).to.equal("");
    expect(el.shadowRoot!.querySelector("#workingDir")!.textContent).to.equal("");
    expect(el.shadowRoot!.querySelector("#initialCommit")!.textContent).to.equal("");
    expect(el.shadowRoot!.querySelector("#messageCount")!.textContent).to.equal("");
    expect(el.shadowRoot!.querySelector("#inputTokens")!.textContent).to.equal("");
    expect(el.shadowRoot!.querySelector("#outputTokens")!.textContent).to.equal("");
    expect(el.shadowRoot!.querySelector("#totalCost")!.textContent).to.equal("$0.00");
  });

  it("renders with partial state data", async () => {
    const partialState: Partial<State> = {
      hostname: "partial-host",
      message_count: 10,
      os: "linux",
      title: "Partial Test",
      total_usage: {
        input_tokens: 500
      }
    };

    const el: SketchContainerStatus = await fixture(html`
      <sketch-container-status .state=${partialState as State}></sketch-container-status>
    `);

    // Check that elements with data are properly populated
    expect(el.shadowRoot!.querySelector("#hostname")!.textContent).to.equal("partial-host");
    expect(el.shadowRoot!.querySelector("#messageCount")!.textContent).to.equal("10");
    expect(el.shadowRoot!.querySelector("#inputTokens")!.textContent).to.equal("500");
    
    // Check that elements without data are empty
    expect(el.shadowRoot!.querySelector("#workingDir")!.textContent).to.equal("");
    expect(el.shadowRoot!.querySelector("#initialCommit")!.textContent).to.equal("");
    expect(el.shadowRoot!.querySelector("#outputTokens")!.textContent).to.equal("");
    expect(el.shadowRoot!.querySelector("#totalCost")!.textContent).to.equal("$0.00");
  });

  it("handles cost formatting correctly", async () => {
    // Test with different cost values
    const testCases = [
      { cost: 0, expected: "$0.00" },
      { cost: 0.1, expected: "$0.10" },
      { cost: 1.234, expected: "$1.23" },
      { cost: 10.009, expected: "$10.01" }
    ];

    for (const testCase of testCases) {
      const stateWithCost = {
        ...mockCompleteState,
        total_usage: {
          ...mockCompleteState.total_usage,
          total_cost_usd: testCase.cost
        }
      };

      const el: SketchContainerStatus = await fixture(html`
        <sketch-container-status .state=${stateWithCost}></sketch-container-status>
      `);

      expect(el.shadowRoot!.querySelector("#totalCost")!.textContent).to.equal(testCase.expected);
    }
  });

  it("truncates commit hash to 8 characters", async () => {
    const stateWithLongCommit = {
      ...mockCompleteState,
      initial_commit: "1234567890abcdef1234567890abcdef12345678"
    };

    const el: SketchContainerStatus = await fixture(html`
      <sketch-container-status .state=${stateWithLongCommit}></sketch-container-status>
    `);

    expect(el.shadowRoot!.querySelector("#initialCommit")!.textContent).to.equal("12345678");
  });

  it("has correct link elements", async () => {
    const el: SketchContainerStatus = await fixture(html`
      <sketch-container-status .state=${mockCompleteState}></sketch-container-status>
    `);

    const links = Array.from(el.shadowRoot!.querySelectorAll('a'));
    expect(links.length).to.equal(2);
    
    // Check for logs link
    const logsLink = links.find(link => link.textContent === 'Logs');
    expect(logsLink).to.exist;
    expect(logsLink!.getAttribute('href')).to.equal('logs');
    
    // Check for download link
    const downloadLink = links.find(link => link.textContent === 'Download');
    expect(downloadLink).to.exist;
    expect(downloadLink!.getAttribute('href')).to.equal('download');
  });
});
