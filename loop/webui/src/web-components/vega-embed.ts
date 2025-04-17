import { css, html, LitElement } from "lit";
import { customElement, property, query } from "lit/decorators.js";
import vegaEmbed from "vega-embed";
import { VisualizationSpec } from "vega-embed";

/**
 * A web component wrapper for vega-embed.
 * Renders Vega and Vega-Lite visualizations.
 *
 * Usage:
 * <vega-embed .spec="${yourVegaLiteSpec}"></vega-embed>
 */
@customElement("vega-embed")
export class VegaEmbed extends LitElement {
  /**
   * The Vega or Vega-Lite specification to render
   */
  @property({ type: Object })
  spec?: VisualizationSpec;

  static styles = css`
    :host {
      display: block;
      width: 100%;
      height: 100%;
    }

    #vega-container {
      width: 100%;
      height: 100%;
      min-height: 200px;
    }
  `;

  @query("#vega-container")
  protected container?: HTMLElement;

  protected firstUpdated() {
    this.renderVegaVisualization();
  }

  protected updated() {
    this.renderVegaVisualization();
  }

  /**
   * Renders the Vega/Vega-Lite visualization using vega-embed
   */
  private async renderVegaVisualization() {
    if (!this.spec) {
      return;
    }

    if (!this.container) {
      return;
    }

    try {
      // Clear previous visualization if any
      this.container.innerHTML = "";

      // Render new visualization
      await vegaEmbed(this.container, this.spec, {
        actions: true,
        renderer: "svg",
      });
    } catch (error) {
      console.error("Error rendering Vega visualization:", error);
      this.container.innerHTML = `<div style="color: red; padding: 10px;">
        Error rendering visualization: ${
          error instanceof Error ? error.message : String(error)
        }
      </div>`;
    }
  }

  render() {
    return html`<div id="vega-container"></div> `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "vega-embed": VegaEmbed;
  }
}
