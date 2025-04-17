import { html, fixture, expect } from "@open-wc/testing";
import "./sketch-network-status";
import type { SketchNetworkStatus } from "./sketch-network-status";

describe("SketchNetworkStatus", () => {
  it("displays the correct connection status when connected", async () => {
    const el: SketchNetworkStatus = await fixture(html`
      <sketch-network-status
        connection="connected"
        message="Connected to server"
      ></sketch-network-status>
    `);

    const indicator = el.shadowRoot!.querySelector(".polling-indicator");
    const statusText = el.shadowRoot!.querySelector(".status-text");

    expect(indicator).to.exist;
    expect(statusText).to.exist;
    expect(indicator!.classList.contains("active")).to.be.true;
    expect(statusText!.textContent).to.equal("Connected to server");
  });

  it("displays the correct connection status when disconnected", async () => {
    const el: SketchNetworkStatus = await fixture(html`
      <sketch-network-status
        connection="disconnected"
        message="Disconnected"
      ></sketch-network-status>
    `);

    const indicator = el.shadowRoot!.querySelector(".polling-indicator");

    expect(indicator).to.exist;
    expect(indicator!.classList.contains("error")).to.be.true;
  });


  it("displays the correct connection status when disabled", async () => {
    const el: SketchNetworkStatus = await fixture(html`
      <sketch-network-status
        connection="disabled"
        message="Disabled"
      ></sketch-network-status>
    `);

    const indicator = el.shadowRoot!.querySelector(".polling-indicator");

    expect(indicator).to.exist;
    expect(indicator!.classList.contains("error")).to.be.false;
    expect(indicator!.classList.contains("active")).to.be.false;
  });

  it("displays error message when provided", async () => {
    const errorMsg = "Connection error";
    const el: SketchNetworkStatus = await fixture(html`
      <sketch-network-status
        connection="disconnected"
        message="Disconnected"
        error="${errorMsg}"
      ></sketch-network-status>
    `);

    const statusText = el.shadowRoot!.querySelector(".status-text");

    expect(statusText).to.exist;
    expect(statusText!.textContent).to.equal(errorMsg);
  });
});
