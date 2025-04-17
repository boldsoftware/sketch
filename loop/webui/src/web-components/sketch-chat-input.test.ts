import { html, fixture, expect, oneEvent, elementUpdated, fixtureCleanup } from "@open-wc/testing";
import "./sketch-chat-input";
import { SketchChatInput } from "./sketch-chat-input";

describe("SketchChatInput", () => {
  afterEach(() => {
    fixtureCleanup();
  });

  it("initializes with empty content by default", async () => {
    const el: SketchChatInput = await fixture(html`
      <sketch-chat-input></sketch-chat-input>
    `);

    expect(el.content).to.equal("");
    const textarea = el.shadowRoot!.querySelector("#chatInput") as HTMLTextAreaElement;
    expect(textarea.value).to.equal("");
  });

  it("initializes with provided content", async () => {
    const testContent = "Hello, world!";
    const el: SketchChatInput = await fixture(html`
      <sketch-chat-input .content=${testContent}></sketch-chat-input>
    `);

    expect(el.content).to.equal(testContent);
    const textarea = el.shadowRoot!.querySelector("#chatInput") as HTMLTextAreaElement;
    expect(textarea.value).to.equal(testContent);
  });

  it("updates content when typing in the textarea", async () => {
    const el: SketchChatInput = await fixture(html`
      <sketch-chat-input></sketch-chat-input>
    `);

    const textarea = el.shadowRoot!.querySelector("#chatInput") as HTMLTextAreaElement;
    const newValue = "New message";
    
    textarea.value = newValue;
    textarea.dispatchEvent(new Event("input"));
    
    expect(el.content).to.equal(newValue);
  });

  it("sends message when clicking the send button", async () => {
    const testContent = "Test message";
    const el: SketchChatInput = await fixture(html`
      <sketch-chat-input .content=${testContent}></sketch-chat-input>
    `);

    const button = el.shadowRoot!.querySelector("#sendChatButton") as HTMLButtonElement;
    
    // Setup listener for the send-chat event
    setTimeout(() => button.click());
    const { detail } = await oneEvent(el, "send-chat");
    
    expect(detail.message).to.equal(testContent);
    expect(el.content).to.equal("");
  });

  it("sends message when pressing Enter (without shift)", async () => {
    const testContent = "Test message";
    const el: SketchChatInput = await fixture(html`
      <sketch-chat-input .content=${testContent}></sketch-chat-input>
    `);

    const textarea = el.shadowRoot!.querySelector("#chatInput") as HTMLTextAreaElement;
    
    // Setup listener for the send-chat event
    setTimeout(() => {
      const enterEvent = new KeyboardEvent("keydown", {
        key: "Enter",
        bubbles: true,
        cancelable: true,
        shiftKey: false
      });
      textarea.dispatchEvent(enterEvent);
    });
    
    const { detail } = await oneEvent(el, "send-chat");
    
    expect(detail.message).to.equal(testContent);
    expect(el.content).to.equal("");
  });

  it("does not send message when pressing Shift+Enter", async () => {
    const testContent = "Test message";
    const el: SketchChatInput = await fixture(html`
      <sketch-chat-input .content=${testContent}></sketch-chat-input>
    `);

    const textarea = el.shadowRoot!.querySelector("#chatInput") as HTMLTextAreaElement;
    
    // Create a flag to track if the event was fired
    let eventFired = false;
    el.addEventListener("send-chat", () => {
      eventFired = true;
    });
    
    // Dispatch the shift+enter keydown event
    const shiftEnterEvent = new KeyboardEvent("keydown", {
      key: "Enter",
      bubbles: true,
      cancelable: true,
      shiftKey: true
    });
    textarea.dispatchEvent(shiftEnterEvent);
    
    // Wait a short time to verify no event was fired
    await new Promise(resolve => setTimeout(resolve, 10));
    
    expect(eventFired).to.be.false;
    expect(el.content).to.equal(testContent);
  });

  it("updates content when receiving update-content event", async () => {
    const el: SketchChatInput = await fixture(html`
      <sketch-chat-input></sketch-chat-input>
    `);

    const newContent = "Updated content";
    
    // Dispatch the update-content event
    const updateEvent = new CustomEvent("update-content", {
      detail: { content: newContent },
      bubbles: true
    });
    el.dispatchEvent(updateEvent);
    
    // Wait for the component to update
    await elementUpdated(el);
    
    expect(el.content).to.equal(newContent);
    const textarea = el.shadowRoot!.querySelector("#chatInput") as HTMLTextAreaElement;
    expect(textarea.value).to.equal(newContent);
  });
});
