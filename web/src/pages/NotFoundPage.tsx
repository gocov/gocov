import { NotFoundState } from "@/components/molecules";
import { usePageTitle } from "@/lib/title";

export default function NotFoundPage() {
  usePageTitle("page not found");
  return <NotFoundState />;
}
