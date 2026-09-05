import { useEffect, useMemo, useState } from "react";
import { Button } from "./button";

export type DualListItem = {
  id: string;
  label: string;
  inherited?: boolean;
  source?: string;
};

export type ResourceAuthorizationDualListLabels = {
  host: string;
  kubernetes: string;
  searchPlaceholder: string;
  available: string;
  authorized: string;
  inherited: string;
  moveSelected: string;
  moveAll: string;
  removeSelected: string;
  removeAll: string;
  previousPage: string;
  nextPage: string;
};

type ResourceType = "host" | "kubernetes_cluster";
type Side = "available" | "authorized";
const PAGE_SIZE = 20;

export function ResourceAuthorizationDualList({
  hosts,
  clusters,
  value,
  labels,
  onChange,
}: {
  hosts: DualListItem[];
  clusters: DualListItem[];
  value: Record<ResourceType, string[]>;
  labels: ResourceAuthorizationDualListLabels;
  onChange: (next: Record<ResourceType, string[]>) => void;
}) {
  const [tab, setTab] = useState<ResourceType>("host");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<
    Record<ResourceType, Record<Side, string[]>>
  >({
    host: { available: [], authorized: [] },
    kubernetes_cluster: { available: [], authorized: [] },
  });
  const items = tab === "host" ? hosts : clusters;
  const authorized = useMemo(() => new Set(value[tab]), [value, tab]);
  const filtered = useMemo(
    () =>
      items.filter((item) =>
        item.label.toLowerCase().includes(query.trim().toLowerCase()),
      ),
    [items, query],
  );
  const available = useMemo(
    () => filtered.filter((item) => !authorized.has(item.id)),
    [filtered, authorized],
  );
  const granted = useMemo(
    () => filtered.filter((item) => authorized.has(item.id)),
    [filtered, authorized],
  );
  const pageCount = (count: number) =>
    Math.max(1, Math.ceil(count / PAGE_SIZE));
  const availablePageCount = pageCount(available.length);
  const grantedPageCount = pageCount(granted.length);
  const pageItems = (list: DualListItem[]) =>
    list.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  useEffect(() => {
    setPage(1);
    setSelected((current) => ({
      ...current,
      [tab]: { available: [], authorized: [] },
    }));
  }, [tab, query]);

  useEffect(() => {
    const max = Math.max(availablePageCount, grantedPageCount);
    if (page > max) setPage(max);
  }, [page, availablePageCount, grantedPageCount]);

  const updateSelection = (side: Side, id: string, checked: boolean) =>
    setSelected((current) => {
      const ids = new Set(current[tab][side]);
      if (checked) ids.add(id);
      else ids.delete(id);
      return { ...current, [tab]: { ...current[tab], [side]: [...ids] } };
    });

  const move = (ids: string[], add: boolean) => {
    if (!ids.length) return;
    const next = new Set(value[tab]);
    ids.forEach((id) => {
      const item = items.find((entry) => entry.id === id);
      if (!item || item.inherited) return;
      if (add) next.add(id);
      else next.delete(id);
    });
    onChange({ ...value, [tab]: [...next] });
    setSelected((current) => ({
      ...current,
      [tab]: { available: [], authorized: [] },
    }));
  };

  const moveAll = (add: boolean) =>
    move(
      (add ? available : granted)
        .filter((item) => !item.inherited)
        .map((item) => item.id),
      add,
    );

  const renderItem = (item: DualListItem, side: Side) => (
    <label className="argus-dual-list__item" key={item.id}>
      <input
        checked={selected[tab][side].includes(item.id)}
        disabled={side === "authorized" && item.inherited}
        onChange={(event) =>
          updateSelection(side, item.id, event.target.checked)
        }
        type="checkbox"
      />
      <span className="argus-dual-list__item-label">{item.label}</span>
      {item.inherited ? (
        <small className="argus-dual-list__item-source">
          {item.source || labels.inherited}
        </small>
      ) : null}
    </label>
  );

  const pager = (count: number) => (
    <div className="argus-dual-list__pager">
      <Button
        disabled={page <= 1}
        onClick={() => setPage((current) => current - 1)}
        size="sm"
        variant="ghost"
      >
        {labels.previousPage}
      </Button>
      <span>
        {page} / {count}
      </span>
      <Button
        disabled={page >= count}
        onClick={() => setPage((current) => current + 1)}
        size="sm"
        variant="ghost"
      >
        {labels.nextPage}
      </Button>
    </div>
  );

  const column = (side: Side, list: DualListItem[], count: number) => {
    const add = side === "available";
    return (
      <section>
        <header>
          <span>
            {add ? labels.available : labels.authorized} ({list.length})
          </span>
          <span>
            <Button
              disabled={!selected[tab][side].length}
              onClick={() => move(selected[tab][side], add)}
              size="sm"
              variant="ghost"
            >
              {add ? labels.moveSelected : labels.removeSelected}
            </Button>
            <Button
              disabled={!list.some((item) => !item.inherited)}
              onClick={() => moveAll(add)}
              size="sm"
              variant="ghost"
            >
              {add ? labels.moveAll : labels.removeAll}
            </Button>
          </span>
        </header>
        {pageItems(list).map((item) => renderItem(item, side))}
        {pager(count)}
      </section>
    );
  };

  return (
    <div className="argus-dual-list" data-resource-type={tab}>
      <div className="argus-dual-list__tabs">
        <Button
          onClick={() => setTab("host")}
          size="sm"
          variant={tab === "host" ? "secondary" : "ghost"}
        >
          {labels.host}
        </Button>
        <Button
          onClick={() => setTab("kubernetes_cluster")}
          size="sm"
          variant={tab === "kubernetes_cluster" ? "secondary" : "ghost"}
        >
          {labels.kubernetes}
        </Button>
      </div>
      <input
        className="argus-input"
        onChange={(event) => setQuery(event.target.value)}
        placeholder={labels.searchPlaceholder}
        value={query}
      />
      <div className="argus-dual-list__columns">
        {column("available", available, availablePageCount)}
        {column("authorized", granted, grantedPageCount)}
      </div>
    </div>
  );
}
