-- 1 when anyone may play the video under URLs that never expire, as well as under
-- signed ones. Setting it back to 0 ends those URLs at once.
ALTER TABLE videos ADD COLUMN public INTEGER NOT NULL DEFAULT 0;
