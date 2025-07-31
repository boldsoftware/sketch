export type ThemeMode = "light" | "dark" | "system";

export class ThemeService {
  private static instance: ThemeService;
  private systemPrefersDark = false;
  private systemMediaQuery: MediaQueryList;

  private constructor() {
    this.systemMediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    this.systemPrefersDark = this.systemMediaQuery.matches;

    // Listen for system theme changes
    this.systemMediaQuery.addEventListener("change", (e) => {
      this.systemPrefersDark = e.matches;
      // If current theme is 'system', update the applied theme
      if (this.getTheme() === "system") {
        this.applyTheme();
      }
    });
  }

  static getInstance(): ThemeService {
    if (!this.instance) {
      this.instance = new ThemeService();
    }
    return this.instance;
  }

  /**
   * Cycle through theme modes: light -> dark -> system -> light
   */
  toggleTheme(): void {
    const currentTheme = this.getTheme();
    let nextTheme: ThemeMode;

    switch (currentTheme) {
      case "light":
        nextTheme = "dark";
        break;
      case "dark":
        nextTheme = "system";
        break;
      case "system":
        nextTheme = "light";
        break;
      default:
        nextTheme = "light";
    }

    this.setTheme(nextTheme);
  }

  /**
   * Set the theme mode
   */
  setTheme(theme: ThemeMode): void {
    // Store the theme preference
    if (theme === "system") {
      localStorage.removeItem("theme");
    } else {
      localStorage.setItem("theme", theme);
    }

    // Apply the theme
    this.applyTheme();

    // Dispatch event for components that need to react
    document.dispatchEvent(
      new CustomEvent("theme-changed", {
        detail: {
          theme,
          effectiveTheme: this.getEffectiveTheme(),
          systemPrefersDark: this.systemPrefersDark,
        },
      }),
    );
  }

  /**
   * Get the current theme preference (light, dark, or system)
   */
  getTheme(): ThemeMode {
    const saved = localStorage.getItem("theme");
    if (saved === "light" || saved === "dark") {
      return saved;
    }
    return "system"; // Default to system preference
  }

  /**
   * Get the effective theme (what is actually applied: light or dark)
   */
  getEffectiveTheme(): "light" | "dark" {
    const theme = this.getTheme();
    if (theme === "system") {
      return this.systemPrefersDark ? "dark" : "light";
    }
    return theme;
  }

  /**
   * Check if dark mode is currently active
   */
  isDarkMode(): boolean {
    return this.getEffectiveTheme() === "dark";
  }

  /**
   * Apply the current theme to the DOM
   */
  private applyTheme(): void {
    const effectiveTheme = this.getEffectiveTheme();
    document.documentElement.classList.toggle(
      "dark",
      effectiveTheme === "dark",
    );
  }

  /**
   * Initialize the theme system
   */
  initializeTheme(): void {
    // Apply the initial theme
    this.applyTheme();
  }
}
