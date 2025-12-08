const get_missions = async ({
  tag = "",
  name = "",
  newer = "2017-06-01",
  older = "2099-12-12",
}: {
  tag?: string;
  name?: string;
  newer?: string;
  older?: string;
} = {}) => {
  try {
    const res = await fetch(
      `${process.env.NEXT_PUBLIC_API_URL}/operations?tag=${tag}&name=${name}&newer=${newer}&older=${older}`,
      {
        next: {
          revalidate: process.env.NODE_ENV === "development" ? 0 : 60,
        },
      }
    );

    console.log("RES:", res);

    // Mission reords data
    return await res.json();
  } catch (error) {
    throw error;
  }
};

export { get_missions };
