/**
 * Escapes HTML special characters in a string
 */
export function escapeHTML(str: string): string {
  return str
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#039;");
}

/**
 * Formats a number with locale-specific formatting
 */
export function formatNumber(
  num: number | null | undefined,
  defaultValue: string = "0",
): string {
  if (num === undefined || num === null) return defaultValue;
  try {
    return num.toLocaleString();
  } catch (e) {
    return String(num);
  }
}

/**
 * Generates a consistent color based on an ID string
 */
export function generateColorFromId(id: string | null | undefined): string {
  if (!id) return "#7c7c7c"; // Default color for null/undefined

  // Generate a hash from the ID
  let hash = 0;
  for (let i = 0; i < id.length; i++) {
    hash = id.charCodeAt(i) + ((hash << 5) - hash);
  }

  // Convert hash to a hex color
  let color = "#";
  for (let i = 0; i < 3; i++) {
    // Generate more muted colors by using only part of the range
    // and adding a base value to avoid very dark colors
    const value = ((hash >> (i * 8)) & 0xff);
    const scaledValue = Math.floor(100 + (value * 100) / 255); // Range 100-200 for more muted colors
    color += scaledValue.toString(16).padStart(2, "0");
  }
  return color;
}
