export interface ZodType {
  readonly _type: string;
}

export type ZodTypeAny = ZodType;

export class ZodString {
  min(length: number): ZodString {
    return this;
  }

  max(length: number): ZodString {
    return this;
  }

  email(message?: string): ZodString {
    return this;
  }
}

export function parse(data: unknown): any {
  return data;
}

export const DEFAULT_ERROR = "Invalid input";

const helper = (x: number) => x + 1;
