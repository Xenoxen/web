import { atom } from "jotai";

import type { Operation } from "@/types";

export const operationsAtom = atom<Operation[]>([]);

export const setOperationsAtom = atom(null, (get, set, update: Operation[]) => {
  set(operationsAtom, update);
});
