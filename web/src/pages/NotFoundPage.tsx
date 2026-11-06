import { PageHeader } from "../components/ui.tsx";
import { Link } from "../router.tsx";

export function NotFoundPage() {
  return (
    <div className="grid gap-6">
      <PageHeader title="Page not found" />
      <Link to="/" className="w-fit text-sm font-semibold text-accent hover:text-accent-hover">
        Back to videos
      </Link>
    </div>
  );
}
