import { html, fixture, expect, oneEvent } from "@open-wc/testing";
import "./sketch-timeline-message";
import type { SketchTimelineMessage } from "./sketch-timeline-message";
import { TimelineMessage, ToolCall, GitCommit, Usage } from "../types";

describe("SketchTimelineMessage", () => {
  // Helper function to create mock timeline messages
  function createMockMessage(props: Partial<TimelineMessage> = {}): TimelineMessage {
    return {
      idx: props.idx || 0,
      type: props.type || "agent",
      content: props.content || "Hello world",
      timestamp: props.timestamp || "2023-05-15T12:00:00Z",
      elapsed: props.elapsed || 1500000000, // 1.5 seconds in nanoseconds
      end_of_turn: props.end_of_turn || false,
      conversation_id: props.conversation_id || "conv123",
      tool_calls: props.tool_calls || [],
      commits: props.commits || [],
      usage: props.usage,
      ...props
    };
  }

  it("renders with basic message content", async () => {
    const message = createMockMessage({
      type: "agent",
      content: "This is a test message"
    });

    const el: SketchTimelineMessage = await fixture(html`
      <sketch-timeline-message
        .message=${message}
      ></sketch-timeline-message>
    `);

    const messageContent = el.shadowRoot!.querySelector(".message-text");
    expect(messageContent).to.exist;
    expect(messageContent!.textContent!.trim()).to.include("This is a test message");
  });

  it("renders with correct message type classes", async () => {
    const messageTypes = ["user", "agent", "tool", "error"];
    
    for (const type of messageTypes) {
      const message = createMockMessage({ type });
      
      const el: SketchTimelineMessage = await fixture(html`
        <sketch-timeline-message
          .message=${message}
        ></sketch-timeline-message>
      `);
      
      const messageElement = el.shadowRoot!.querySelector(".message");
      expect(messageElement).to.exist;
      expect(messageElement!.classList.contains(type)).to.be.true;
    }
  });

  it("renders end-of-turn marker correctly", async () => {
    const message = createMockMessage({
      end_of_turn: true
    });

    const el: SketchTimelineMessage = await fixture(html`
      <sketch-timeline-message
        .message=${message}
      ></sketch-timeline-message>
    `);

    const messageElement = el.shadowRoot!.querySelector(".message");
    expect(messageElement).to.exist;
    expect(messageElement!.classList.contains("end-of-turn")).to.be.true;
  });

  it("formats timestamps correctly", async () => {
    const message = createMockMessage({
      timestamp: "2023-05-15T12:00:00Z"
    });

    const el: SketchTimelineMessage = await fixture(html`
      <sketch-timeline-message
        .message=${message}
      ></sketch-timeline-message>
    `);

    const timestamp = el.shadowRoot!.querySelector(".message-timestamp");
    expect(timestamp).to.exist;
    // Should include a formatted date like "May 15, 2023"
    expect(timestamp!.textContent).to.include("May 15, 2023");
    // Should include elapsed time
    expect(timestamp!.textContent).to.include("(1.50s)");
  });

  it("renders markdown content correctly", async () => {
    const markdownContent = "# Heading\n\n- List item 1\n- List item 2\n\n`code block`";
    const message = createMockMessage({
      content: markdownContent
    });

    const el: SketchTimelineMessage = await fixture(html`
      <sketch-timeline-message
        .message=${message}
      ></sketch-timeline-message>
    `);

    const contentElement = el.shadowRoot!.querySelector(".markdown-content");
    expect(contentElement).to.exist;
    expect(contentElement!.innerHTML).to.include("<h1>Heading</h1>");
    expect(contentElement!.innerHTML).to.include("<ul>");
    expect(contentElement!.innerHTML).to.include("<li>List item 1</li>");
    expect(contentElement!.innerHTML).to.include("<code>code block</code>");
  });

  it("displays usage information when available", async () => {
    const usage: Usage = {
      input_tokens: 150,
      output_tokens: 300,
      cost_usd: 0.025,
      cache_read_input_tokens: 50
    };
    
    const message = createMockMessage({
      usage
    });

    const el: SketchTimelineMessage = await fixture(html`
      <sketch-timeline-message
        .message=${message}
      ></sketch-timeline-message>
    `);

    const usageElement = el.shadowRoot!.querySelector(".message-usage");
    expect(usageElement).to.exist;
    expect(usageElement!.textContent).to.include("In: 150");
    expect(usageElement!.textContent).to.include("Out: 300");
    expect(usageElement!.textContent).to.include("Cache: 50");
    expect(usageElement!.textContent).to.include("$0.03");
  });

  it("renders commit information correctly", async () => {
    const commits: GitCommit[] = [
      {
        hash: "1234567890abcdef",
        subject: "Fix bug in application",
        body: "This fixes a major bug in the application\n\nSigned-off-by: Developer",
        pushed_branch: "main"
      }
    ];
    
    const message = createMockMessage({
      commits
    });

    const el: SketchTimelineMessage = await fixture(html`
      <sketch-timeline-message
        .message=${message}
      ></sketch-timeline-message>
    `);

    const commitsContainer = el.shadowRoot!.querySelector(".commits-container");
    expect(commitsContainer).to.exist;
    
    const commitHeader = commitsContainer!.querySelector(".commits-header");
    expect(commitHeader).to.exist;
    expect(commitHeader!.textContent).to.include("1 new commit");
    
    const commitHash = commitsContainer!.querySelector(".commit-hash");
    expect(commitHash).to.exist;
    expect(commitHash!.textContent).to.equal("12345678"); // First 8 chars
    
    const pushedBranch = commitsContainer!.querySelector(".pushed-branch");
    expect(pushedBranch).to.exist;
    expect(pushedBranch!.textContent).to.include("main");
  });

  it("dispatches show-commit-diff event when commit diff button is clicked", async () => {
    const commits: GitCommit[] = [
      {
        hash: "1234567890abcdef",
        subject: "Fix bug in application",
        body: "This fixes a major bug in the application",
        pushed_branch: "main"
      }
    ];
    
    const message = createMockMessage({
      commits
    });

    const el: SketchTimelineMessage = await fixture(html`
      <sketch-timeline-message
        .message=${message}
      ></sketch-timeline-message>
    `);

    const diffButton = el.shadowRoot!.querySelector(".commit-diff-button") as HTMLButtonElement;
    expect(diffButton).to.exist;
    
    // Set up listener for the event
    setTimeout(() => diffButton!.click());
    const { detail } = await oneEvent(el, "show-commit-diff");
    
    expect(detail).to.exist;
    expect(detail.commitHash).to.equal("1234567890abcdef");
  });

  it("handles message type icon display correctly", async () => {
    // First message of a type should show icon
    const firstMessage = createMockMessage({
      type: "user",
      idx: 0
    });
    
    // Second message of same type should not show icon
    const secondMessage = createMockMessage({
      type: "user",
      idx: 1
    });

    // Test first message (should show icon)
    const firstEl: SketchTimelineMessage = await fixture(html`
      <sketch-timeline-message
        .message=${firstMessage}
      ></sketch-timeline-message>
    `);

    const firstIcon = firstEl.shadowRoot!.querySelector(".message-icon");
    expect(firstIcon).to.exist;
    expect(firstIcon!.textContent!.trim()).to.equal("U");

    // Test second message with previous message of same type
    const secondEl: SketchTimelineMessage = await fixture(html`
      <sketch-timeline-message
        .message=${secondMessage}
        .previousMessage=${firstMessage}
      ></sketch-timeline-message>
    `);

    const secondIcon = secondEl.shadowRoot!.querySelector(".message-icon");
    expect(secondIcon).to.not.exist;
  });

  it("formats numbers correctly", async () => {
    const el: SketchTimelineMessage = await fixture(html`
      <sketch-timeline-message></sketch-timeline-message>
    `);

    // Test accessing private method via the component instance
    expect(el.formatNumber(1000)).to.equal("1,000");
    expect(el.formatNumber(null, "N/A")).to.equal("N/A");
    expect(el.formatNumber(undefined, "--")).to.equal("--");
  });

  it("formats currency values correctly", async () => {
    const el: SketchTimelineMessage = await fixture(html`
      <sketch-timeline-message></sketch-timeline-message>
    `);

    // Test with different precisions
    expect(el.formatCurrency(10.12345, "$0.00", true)).to.equal("$10.1235"); // message level (4 decimals)
    expect(el.formatCurrency(10.12345, "$0.00", false)).to.equal("$10.12"); // total level (2 decimals)
    expect(el.formatCurrency(null, "N/A")).to.equal("N/A");
    expect(el.formatCurrency(undefined, "--")).to.equal("--");
  });
});
