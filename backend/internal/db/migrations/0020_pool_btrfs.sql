-- Pool-Typ: mergerfs (Standard) oder btrfs. Bei btrfs gehören die Mitgliedsplatten zu einem
-- gemeinsamen Mehrplatten-Dateisystem (fs_uuid) mit Profil (single, raid1, raid1c3, raid10).
ALTER TABLE pools ADD COLUMN type TEXT NOT NULL DEFAULT 'mergerfs';
ALTER TABLE pools ADD COLUMN profile TEXT NOT NULL DEFAULT '';
ALTER TABLE pools ADD COLUMN fs_uuid TEXT NOT NULL DEFAULT '';
ALTER TABLE pools ADD COLUMN compress TEXT NOT NULL DEFAULT '';
