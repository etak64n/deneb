-- 1 when the video plays under permanent URLs, which never expire, as well as under
-- signed ones, which do. Setting it back to 0 ends the permanent URLs at once.
ALTER TABLE videos ADD COLUMN permanent_urls INTEGER NOT NULL DEFAULT 0;
