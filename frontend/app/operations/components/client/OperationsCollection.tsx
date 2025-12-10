"use client";

import debounce from "debounce";
import { useEffect, useState } from "react";

// Components
import OperationCard from "@/app/operations/components/client/OperationCard";

// Types
import type { ChangeEvent } from "react";

// Jotai
import { useAtomValue } from "jotai";
import { operationsAtom } from "@/atoms/operations.atom";
import { Operation } from "@/types";

export default function OperationsCollection() {
  const operations = useAtomValue(operationsAtom);
  const [filteredOperations, setFilteredOperations] =
    useState<Operation[]>(operations);

  useEffect(() => {
    setFilteredOperations(operations);
  }, [operations]);

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
        setFilteredOperations(filtered);
        break;

      default:
        break;
    }
    setFilteredOperations(filtered);
  };

  // Debounced search handler
  const debouncedSearch = debounce((val: string) => {
    const filtered = operations.filter((operation) => {
      return operation.mission_name.toLowerCase().includes(val);
    });
    setFilteredOperations(filtered);
  }, 300);

  const onSearch = (e: ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value.toLowerCase();
    debouncedSearch(val);
  };

  return (
    <div className="h-full">
      <div className="w-full mb-4 bg-gray-800 p-2">
        <div className="flex items-center gap-2">
          <select
            className="bg-gray-900 text-white p-2 rounded-lg"
            onChange={(e) => filter(e, "tag")}
          >
            <option value="">All</option>
            <option value="active">Active</option>
            <option value="completed">Completed</option>
            <option value="failed">Failed</option>
          </select>
          <input
            type="text"
            placeholder="Search"
            className="w-full bg-gray-900 text-white p-2 rounded-lg"
            onChange={onSearch}
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
      {filteredOperations?.length > 0 ? (
        <div className="grid grid-cols-12 gap-4 overflow-y-auto max-h-full">
          {filteredOperations.map((operation: Operation) => {
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
