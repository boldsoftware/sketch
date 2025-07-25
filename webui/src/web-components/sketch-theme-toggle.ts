import { html } from "lit";
import { customElement, state } from "lit/decorators.js";
import { SketchTailwindElement } from "./sketch-tailwind-element.js";
import { ThemeService, ThemeMode } from "./theme-service.js";

@customElement("sketch-theme-toggle")
export class SketchThemeToggle extends SketchTailwindElement {
  @state() private currentTheme: ThemeMode = "system";
  @state() private effectiveTheme: "light" | "dark" = "light";

  private themeService = ThemeService.getInstance();

  connectedCallback() {
    super.connectedCallback();
    this.updateThemeState();

    // Listen for theme changes from other sources
    document.addEventListener("theme-changed", this.handleThemeChange);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener("theme-changed", this.handleThemeChange);
  }

  private handleThemeChange = (e: CustomEvent) => {
    this.currentTheme = e.detail.theme;
    this.effectiveTheme = e.detail.effectiveTheme;
  };

  private updateThemeState() {
    this.currentTheme = this.themeService.getTheme();
    this.effectiveTheme = this.themeService.getEffectiveTheme();
  }

  private toggleTheme() {
    this.themeService.toggleTheme();
  }

  private getThemeIcon(): string {
    switch (this.currentTheme) {
      case "light":
        return "\u2600\ufe0f"; // Sun
      case "dark":
        return "\ud83c\udf19"; // Moon
      case "system":
        return "\ud83d\udcbb"; // Computer/Laptop
      default:
        return "\ud83d\udcbb";
    }
  }

  private getThemeLabel(): string {
    switch (this.currentTheme) {
      case "light":
        return "Light mode";
      case "dark":
        return "Dark mode";
      case "system":
        return `System theme (${this.effectiveTheme})`;
      default:
        return "System theme";
    }
  }

  private getNextThemeLabel(): string {
    switch (this.currentTheme) {
      case "light":
        return "Switch to dark mode";
      case "dark":
        return "Switch to system theme";
      case "system":
        return "Switch to light mode";
      default:
        return "Switch theme";
    }
  }

  render() {
    return html`
      <button
        @click=${this.toggleTheme}
        class="p-2 rounded-md border border-gray-300 dark:border-gray-600 
               bg-white dark:bg-gray-800 text-gray-700 dark:text-gray-200
               hover:bg-gray-50 dark:hover:bg-gray-700 transition-colors
               focus:outline-none focus:ring-2 focus:ring-blue-500"
        title="${this.getThemeLabel()} - ${this.getNextThemeLabel()}"
        aria-label="${this.getNextThemeLabel()}"
      >
        ${this.getThemeIcon()}
      </button>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-theme-toggle": SketchThemeToggle;
  }
}
