export type Lang = 'vi' | 'en'

/** Translate function: resolves a key and interpolates {vars}. */
export type T = (key: string, vars?: Record<string, string | number>) => string
