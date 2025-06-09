import { test, expect } from "@sand4rt/experimental-ct-web";
import { MobileChat } from "./mobile-chat";
import { AgentMessage } from "../types";

// Helper function to create mock messages
function createMockMessage(props: Partial<AgentMessage> = {}): AgentMessage {
  return {
    idx: props.idx || 0,
    type: props.type || "agent",
    content: props.content || "Hello world",
    timestamp: props.timestamp || "2023-05-15T12:00:00Z",
    elapsed: props.elapsed || 1500000000, // 1.5 seconds in nanoseconds
    end_of_turn: props.end_of_turn || false,
    conversation_id: props.conversation_id || "conv123",
    ...props,
  };
}

test("renders basic chat messages", async ({ mount }) => {
  const messages = [
    createMockMessage({
      type: "user",
      content: "Hello, this is a user message",
    }),
    createMockMessage({
      type: "agent",
      content: "Hello, this is an agent response",
    }),
  ];

  const component = await mount(MobileChat, {
    props: {
      messages: messages,
    },
  });

  await expect(component.locator(".message.user")).toBeVisible();
  await expect(component.locator(".message.assistant")).toBeVisible();
  await expect(component.locator(".message.user .message-bubble")).toContainText(
    "Hello, this is a user message",
  );
  await expect(
    component.locator(".message.assistant .message-bubble"),
  ).toContainText("Hello, this is an agent response");
});

test("renders error messages with red styling", async ({ mount }) => {
  const messages = [
    createMockMessage({
      type: "error",
      content: "This is an error message",
    }),
  ];

  const component = await mount(MobileChat, {
    props: {
      messages: messages,
    },
  });

  // Check that error message is visible
  await expect(component.locator(".message.error")).toBeVisible();
  await expect(component.locator(".message.error .message-bubble")).toContainText(
    "This is an error message",
  );

  // Check that error message has red styling
  const errorBubble = component.locator(".message.error .message-bubble");
  await expect(errorBubble).toBeVisible();
  
  // Verify the background color and text color
  const bgColor = await errorBubble.evaluate((el) => {
    return window.getComputedStyle(el).backgroundColor;
  });
  const textColor = await errorBubble.evaluate((el) => {
    return window.getComputedStyle(el).color;
  });
  
  // Check that we have red-ish colors (these will be RGB values)
  expect(bgColor).toMatch(/rgb\(255, 235, 238\)/); // #ffebee
  expect(textColor).toMatch(/rgb\(211, 47, 47\)/); // #d32f2f
});

test("filters messages correctly", async ({ mount }) => {
  const messages = [
    createMockMessage({
      type: "user",
      content: "User message",
    }),
    createMockMessage({
      type: "agent",
      content: "Agent message",
    }),
    createMockMessage({
      type: "error",
      content: "Error message",
    }),
    createMockMessage({
      type: "tool",
      content: "", // Empty content should be filtered out
    }),
    createMockMessage({
      type: "agent",
      content: "   ", // Whitespace-only content should be filtered out
    }),
  ];

  const component = await mount(MobileChat, {
    props: {
      messages: messages,
    },
  });

  // Should show user, agent, and error messages with content
  await expect(component.locator(".message.user")).toBeVisible();
  await expect(component.locator(".message.assistant")).toHaveCount(1); // Only one agent message with content
  await expect(component.locator(".message.error")).toBeVisible();
});

test("shows thinking indicator", async ({ mount }) => {
  const component = await mount(MobileChat, {
    props: {
      messages: [],
      isThinking: true,
    },
  });

  await expect(component.locator(".thinking-message")).toBeVisible();
  await expect(component.locator(".thinking-text")).toContainText(
    "Sketch is thinking",
  );
  await expect(component.locator(".thinking-dots")).toBeVisible();
});

test("shows empty state when no messages", async ({ mount }) => {
  const component = await mount(MobileChat, {
    props: {
      messages: [],
      isThinking: false,
    },
  });

  await expect(component.locator(".empty-state")).toBeVisible();
  await expect(component.locator(".empty-state")).toContainText(
    "Start a conversation with Sketch...",
  );
});

test("renders markdown content in assistant messages", async ({ mount }) => {
  const messages = [
    createMockMessage({
      type: "agent",
      content: "# Heading\n\n- List item 1\n- List item 2\n\n`code block`",
    }),
  ];

  const component = await mount(MobileChat, {
    props: {
      messages: messages,
    },
  });

  await expect(component.locator(".markdown-content")).toBeVisible();
  
  // Check that markdown is rendered as HTML
  const html = await component
    .locator(".markdown-content")
    .evaluate((element) => element.innerHTML);
  expect(html).toContain("<h1>");
  expect(html).toContain("<ul>");
  expect(html).toContain("<code>");
});
