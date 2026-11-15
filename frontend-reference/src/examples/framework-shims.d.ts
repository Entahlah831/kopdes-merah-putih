declare module "react" {
  export function useState<T>(initial: T | (() => T)): [T, (next: T | ((prev: T) => T)) => void];
  export function useEffect(effect: () => void | (() => void), deps?: readonly unknown[]): void;
  export function useCallback<T extends (...args: any[]) => any>(fn: T, deps: readonly unknown[]): T;
  export namespace JSX {
    interface Element {}
    interface IntrinsicElements {
      [elemName: string]: any;
    }
  }
  export type FormEvent<T = unknown> = {
    preventDefault(): void;
    currentTarget: T;
  };
  export type ChangeEvent<T = unknown> = {
    currentTarget: T;
  };
}

declare module "react/jsx-runtime" {
  export function jsx(type: any, props: any, key?: any): any;
  export function jsxs(type: any, props: any, key?: any): any;
  export const Fragment: any;
  export namespace JSX {
    interface Element {}
    interface IntrinsicElements {
      [elemName: string]: any;
    }
  }
}

declare module "vue" {
  export interface Ref<T> {
    value: T;
  }
  export function ref<T>(initial: T): Ref<T>;
  export function computed<T>(getter: () => T): { readonly value: T };
  export function onUnmounted(cb: () => void): void;
}
