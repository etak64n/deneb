import { PageHeader } from "../components/ui.tsx";
import { Link, useTitle } from "../router.tsx";

export function NotFoundPage() {
  useTitle("Page not found");
  return (
    <div className="grid gap-6">
      <PageHeader title="Page not found" />
      <Link to="/" className="w-fit text-sm font-semibold text-accent hover:text-accent-hover">
        Back to videos
      </Link>
    </div>
  );
}
