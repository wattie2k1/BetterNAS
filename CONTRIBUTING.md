# Mitwirken an BetterNAS

Danke, dass du BetterNAS verbessern möchtest.

## Lizenz der Beiträge

Der Kern von BetterNAS steht unter der **GNU General Public License, Version 3 oder neuer (GPL-3.0-or-later)**,
siehe [LICENSE](LICENSE). Der Maintainer kann zusätzlich nicht öffentliche Zusatzmodule unter einer anderen Lizenz
anbieten (zum Beispiel Optionen, die per Spende freigeschaltet werden).

Mit deinem Beitrag bestätigst du:

1. Du hast das Recht, den Beitrag unter diesen Bedingungen einzureichen (Developer Certificate of Origin,
   <https://developercertificate.org/>).
2. Dein Beitrag wird als Teil des Kerns unter GPL-3.0-or-later veröffentlicht.
3. Der Maintainer darf deinen Beitrag auch in diesen nicht öffentlichen Zusatzmodulen und in künftigen
   Lizenzfassungen des Kerns verwenden.

## So reichst du einen Beitrag ein

- Jeder Commit trägt eine `Signed-off-by: Name <E-Mail>`-Zeile (`git commit -s`). Sie gilt als Zustimmung zu den
  Punkten oben.
- Änderungen kommen per Pull-Request gegen `main`. Tests müssen grün sein: `make test`.
- Alle Texte der Oberfläche und der Fehlermeldungen sind deutsch.
- Neue Funktionen folgen dem vorhandenen Muster: API validiert → typisierter Agent-RPC → Renderer mit Rollback →
  Audit-Eintrag.
