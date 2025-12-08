"use client";

import { useState } from "react";

// Components
import Mission from "./Mission";

// Types
import type { mission } from "@/app/missions/types";
import type { ChangeEvent } from "react";

export default function MissionsCollection({ data = [] }: { data: mission[] }) {
  const [missions, setMissions] = useState<mission[]>(data);

  const filter = (
    e: ChangeEvent<HTMLInputElement | HTMLSelectElement>,
    type: string
  ) => {
    const value = e.target.value.toLowerCase();
    let filtered: mission[] = [];

    switch (type) {
      case "tag":
        filtered = data.filter((mission) => {
          return mission.tag.toLowerCase().includes(value);
        });
        setMissions(filtered);
        break;

      default:
        break;
    }
    setMissions(filtered);
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
      {missions.length > 0 ? (
        <div className="grid grid-cols-12 gap-4 overflow-y-auto max-h-full">
          {missions.map((mission: mission) => {
            return (
              <div className="col-span-4">
                <Mission data={mission} />
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
