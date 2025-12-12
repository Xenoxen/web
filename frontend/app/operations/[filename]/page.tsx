export const generateStaticParams = async () => {
  const tag = "";
  const name = "";
  const newer = "2017-06-01";
  const older = "2099-12-12";

  const apiUrl = process.env.NEXT_PUBLIC_API_URL || "http://localhost:5000";
  const requestUrl = `${apiUrl}/operations?tag=${tag}&name=${name}&newer=${newer}&older=${older}`;
  console.log("Request URL: ", requestUrl);
  const res = await fetch(requestUrl, { credentials: "include" }).then((res) => res.json());
  console.log("Fetch Res: ", res);
  const operations = res.data
  console.log("Operations: ", operations);

  return operations.map((operation: { filename: string }) => ({
    filename: operation.filename,
  }));
};

export default function OperationPage() {
  return <div id="map" className="w-full h-full" />;
}
