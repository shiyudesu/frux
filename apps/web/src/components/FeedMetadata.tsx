import type { FeedVideo } from "../types";
import type { PublicProfileInput } from "../utils";

interface FeedMetadataProps {
  item: FeedVideo;
  followError: string;
  onOpenAuthor: (profile: PublicProfileInput) => void;
}

export function FeedMetadata({ item, followError, onOpenAuthor }: FeedMetadataProps) {
  return (
    <div className="stage-copy" data-ui="feed-metadata">
      <button className="metadata-author" type="button" onClick={() => onOpenAuthor({
        author_id: item.author_id,
        author: item.author,
        avatar_url: item.avatar_url,
        description: item.description
      })}>
        @{item.author}
      </button>
      {followError && <p className="stage-notice">{followError}</p>}
      <h1>{item.title}</h1>
      <p>{item.description}</p>
    </div>
  );
}
