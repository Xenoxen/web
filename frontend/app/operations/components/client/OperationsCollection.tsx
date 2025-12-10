"use client";

// Components
import OperationCard from "@/app/operations/components/client/OperationCard";

// Types
import type { ChangeEvent } from "react";

// Jotai
import { useAtom } from "jotai";
import { operationsAtom } from "@/atoms/operations.atom";
import { Operation } from "@/types";

export default function OperationsCollection() {
  const [operations, setOperations] = useAtom(operationsAtom);

  const filter = (
    e: ChangeEvent<HTMLInputElement | HTMLSelectElement>,
    type: string
  ) => {
    const value = e.target.value.toLowerCase();
    let filtered: Operation[] = [];

    switch (type) {
      case "tag":
        filtered = operations.filter((operation) => {
          return operation.tag.toLowerCase().includes(value);
        });
        setOperations(filtered);
        break;

      default:
        break;
    }
    setOperations(filtered);
  };

  return (
    <div className="h-full">
      <div className="w-full mb-4 bg-gray-800 p-2">
        <div className="flex items-center gap-2">
          <select
            className="bg-gray-900 text-white p-2 rounded-lg"
            onChange={(e) => filter(e, "tag")}
          >
            <option value="all">All</option>
            <option value="active">Active</option>
            <option value="completed">Completed</option>
            <option value="failed">Failed</option>
          </select>
          <input
            type="text"
            placeholder="Search"
            className="w-full bg-gray-900 text-white p-2 rounded-lg"
          />
          <input
            type="date"
            className="bg-gray-900 text-white p-2 rounded-lg min-w-[140px]"
          />
          <input
            type="date"
            className="bg-gray-900 text-white p-2 rounded-lg min-w-[140px]"
          />
        </div>
      </div>
      {operations?.length > 0 ? (
        <div className="grid grid-cols-12 gap-4 overflow-y-auto max-h-full">
          {operations.map((operation: Operation) => {
            return (
              <div key={operation.id} className="col-span-4">
                <OperationCard data={operation} />
              </div>
            );
          })}
        </div>
      ) : (
        <div className="h-full flex items-center justify-center">
          <div>
            <h1 className="text-4xl font-bold text-center stext-white uppercase">
              No missions found
            </h1>
            <p className="text-white text-center">
              Create a new mission to get started
            </p>
          </div>
        </div>
      )}
    </div>
  );
}
