"use client";

import Link from "next/link";
import useSWR from "swr";
import { useEffect } from "react";

// Components
import OperationsCollection from "@/app/client/OperationsCollection";

// Jotai
import { useSetAtom } from "jotai";
import { setOperationsAtom } from "@/atoms/operations.atom";
import { Operation } from "@/types";

const fetcher = (url: string) =>
  fetch(url, {
    credentials: "include",
  }).then((res) => res.json());

export default function Missions() {
  const tag = "";
  const name = "";
  const newer = "2017-06-01";
  const older = "2099-12-12";

  const apiUrl = process.env.NEXT_PUBLIC_API_URL || "http://localhost:5000";

  const {
    data,
    error = null,
    isLoading,
  } = useSWR(
    `${apiUrl}/operations?tag=${tag}&name=${name}&newer=${newer}&older=${older}`,
    fetcher
  );

  const setOperations = useSetAtom(setOperationsAtom);

  useEffect(() => {
    // Filter data by date (DESC)
    const operations = data?.sort((a: Operation, b: Operation) => {
      return new Date(b.date).getTime() - new Date(a.date).getTime();
    });
    setOperations(operations || []);
  }, [data, setOperations]);

  // Loading screen
  if (isLoading) {
    return (
      <main className="h-full flex items-center justify-center">
        Loading...
      </main>
    );
  }

  // Error screen
  if (error) {
    return (
      <main className="h-full flex items-center justify-center">
        Failed to load missions: {error?.message ?? "Unknown error"}
      </main>
    );
  }

  return (
    <main className="h-full">
      {/* Navigation */}
      <nav className="bg-black p-4 w-full">
        <div className="container mx-auto flex justify-between items-center">
          <h1 className="text-sm uppercase font-bold text-white">OCAP2</h1>
          <ul className="flex space-x-4">
            <li>
              <Link href="/operations" className="text-sm uppercase text-white">
                Mission Records
              </Link>
            </li>
          </ul>
        </div>
      </nav>

      {/* Content */}
      <div className="h-full container mx-auto">
        <div className="bg-gray-900 p-4 rounded-lg shadow-lg mb-5">
          <h1 className="text-3xl font-bold text-white text-center">
            Mission Records
          </h1>
        </div>

        {/* Missions */}
        <OperationsCollection />
      </div>
    </main>
  );
}
