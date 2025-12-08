"use client";

// Types
import { mission } from "@/app/missions/types";

export default function Mission({ data }: { data: mission }) {
  return (
    <div className="mission bg-gray-800 p-4 cursor-pointer transition-transform duration-100 active:scale-95">
      
      <h2 className="text-lg font-medium">{data.mission_name}</h2>
      <p>{data.world_name}</p>
      <p>{data.mission_duration}</p>
      <p>{new Date(data.date).toLocaleDateString("en-US", {
        year: "numeric",
        month: "long",
        day: "numeric",
      })}</p>
    </div>
  );
}
