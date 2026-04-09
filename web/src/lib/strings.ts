/**
 * Trims leading/trailing whitespace from all string values in an object.
 * Useful for cleaning form data before sending to the API.
 * Non-string values and nested objects are left unchanged.
 */
export function trimFields<T extends Record<string, unknown>>(obj: T): T {
  const result = { ...obj };
  for (const key in result) {
    if (typeof result[key] === "string") {
      (result as Record<string, unknown>)[key] = (result[key] as string).trim();
    }
  }
  return result;
}
