import { html, fixture, expect, oneEvent, elementUpdated, fixtureCleanup } from "@open-wc/testing";
import "./sketch-view-mode-select";
import type { SketchViewModeSelect } from "./sketch-view-mode-select";

describe("SketchViewModeSelect", () => {
  afterEach(() => {
    fixtureCleanup();
  });

  it("initializes with 'chat' as the default mode", async () => {
    const el: SketchViewModeSelect = await fixture(html`
      <sketch-view-mode-select></sketch-view-mode-select>
    `);

    expect(el.activeMode).to.equal("chat");
    const chatButton = el.shadowRoot!.querySelector("#showConversationButton");
    expect(chatButton!.classList.contains("active")).to.be.true;
  });

  it("displays all four view mode buttons", async () => {
    const el: SketchViewModeSelect = await fixture(html`
      <sketch-view-mode-select></sketch-view-mode-select>
    `);

    const buttons = el.shadowRoot!.querySelectorAll(".emoji-button");
    expect(buttons.length).to.equal(4);

    const chatButton = el.shadowRoot!.querySelector("#showConversationButton");
    const diffButton = el.shadowRoot!.querySelector("#showDiffButton");
    const chartsButton = el.shadowRoot!.querySelector("#showChartsButton");
    const terminalButton = el.shadowRoot!.querySelector("#showTerminalButton");

    expect(chatButton).to.exist;
    expect(diffButton).to.exist;
    expect(chartsButton).to.exist;
    expect(terminalButton).to.exist;

    expect(chatButton!.getAttribute("title")).to.equal("Conversation View");
    expect(diffButton!.getAttribute("title")).to.equal("Diff View");
    expect(chartsButton!.getAttribute("title")).to.equal("Charts View");
    expect(terminalButton!.getAttribute("title")).to.equal("Terminal View");
  });

  it("dispatches view-mode-select event when clicking a mode button", async () => {
    const el: SketchViewModeSelect = await fixture(html`
      <sketch-view-mode-select></sketch-view-mode-select>
    `);

    const diffButton = el.shadowRoot!.querySelector("#showDiffButton") as HTMLButtonElement;
    
    // Setup listener for the view-mode-select event
    setTimeout(() => diffButton.click());
    const { detail } = await oneEvent(el, "view-mode-select");
    
    expect(detail.mode).to.equal("diff");
  });

  it("updates the active mode when receiving update-active-mode event", async () => {
    const el: SketchViewModeSelect = await fixture(html`
      <sketch-view-mode-select></sketch-view-mode-select>
    `);

    // Initially should be in chat mode
    expect(el.activeMode).to.equal("chat");
    
    // Dispatch the update-active-mode event to change to diff mode
    const updateEvent = new CustomEvent("update-active-mode", {
      detail: { mode: "diff" },
      bubbles: true
    });
    el.dispatchEvent(updateEvent);
    
    // Wait for the component to update
    await elementUpdated(el);
    
    expect(el.activeMode).to.equal("diff");
    const diffButton = el.shadowRoot!.querySelector("#showDiffButton");
    expect(diffButton!.classList.contains("active")).to.be.true;
  });

  it("correctly marks the active button based on mode", async () => {
    const el: SketchViewModeSelect = await fixture(html`
      <sketch-view-mode-select activeMode="terminal"></sketch-view-mode-select>
    `);

    // Terminal button should be active
    const terminalButton = el.shadowRoot!.querySelector("#showTerminalButton");
    const chatButton = el.shadowRoot!.querySelector("#showConversationButton");
    const diffButton = el.shadowRoot!.querySelector("#showDiffButton");
    const chartsButton = el.shadowRoot!.querySelector("#showChartsButton");
    
    expect(terminalButton!.classList.contains("active")).to.be.true;
    expect(chatButton!.classList.contains("active")).to.be.false;
    expect(diffButton!.classList.contains("active")).to.be.false;
    expect(chartsButton!.classList.contains("active")).to.be.false;
  });


});
