import { useEffect, useMemo, useState } from "react";
import { Button } from "./button";

export type DualListItem = { id: string; label: string; inherited?: boolean; source?: string };
type ResourceType = "host" | "kubernetes_cluster";
type Side = "available" | "authorized";
const PAGE_SIZE = 20;

export function ResourceAuthorizationDualList({ hosts, clusters, value, onChange }: { hosts: DualListItem[]; clusters: DualListItem[]; value: Record<ResourceType, string[]>; onChange: (next: Record<ResourceType, string[]>) => void }) {
  const [tab, setTab] = useState<ResourceType>("host");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Record<ResourceType, Record<Side, string[]>>>({ host: { available: [], authorized: [] }, kubernetes_cluster: { available: [], authorized: [] } });
  const items = tab === "host" ? hosts : clusters;
  const authorized = useMemo(() => new Set(value[tab]), [value, tab]);
  const filtered = useMemo(() => items.filter((item) => item.label.toLowerCase().includes(query.trim().toLowerCase())), [items, query]);
  const available = useMemo(() => filtered.filter((item) => !authorized.has(item.id)), [filtered, authorized]);
  const granted = useMemo(() => filtered.filter((item) => authorized.has(item.id)), [filtered, authorized]);
  const pageCount = (count: number) => Math.max(1, Math.ceil(count / PAGE_SIZE));
  const availablePageCount = pageCount(available.length);
  const grantedPageCount = pageCount(granted.length);
  const pageItems = (list: DualListItem[]) => list.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

  useEffect(() => { setPage(1); setSelected((current) => ({ ...current, [tab]: { available: [], authorized: [] } })); }, [tab, query]);
  useEffect(() => { const max = Math.max(availablePageCount, grantedPageCount); if (page > max) setPage(max); }, [page, availablePageCount, grantedPageCount]);
  const updateSelection = (side: Side, id: string, checked: boolean) => setSelected((current) => {
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
    setSelected((current) => ({ ...current, [tab]: { available: [], authorized: [] } }));
  };
  const moveAll = (add: boolean) => move((add ? available : granted).filter((item) => !item.inherited).map((item) => item.id), add);
  const renderItem = (item: DualListItem, side: Side) => <label className="argus-dual-list__item" key={item.id}><input type="checkbox" checked={selected[tab][side].includes(item.id)} disabled={side === "authorized" && item.inherited} onChange={(event) => updateSelection(side, item.id, event.target.checked)} /><span className="argus-dual-list__item-label">{item.label}</span>{item.inherited ? <small className="argus-dual-list__item-source">{item.source || "继承授权"}</small> : null}</label>;
  const pager = (count: number) => <div className="argus-dual-list__pager"><Button size="sm" variant="ghost" disabled={page <= 1} onClick={() => setPage((current) => current - 1)}>上一页</Button><span>{page} / {count}</span><Button size="sm" variant="ghost" disabled={page >= count} onClick={() => setPage((current) => current + 1)}>下一页</Button></div>;
  return <div className="argus-dual-list" data-resource-type={tab}><div className="argus-dual-list__tabs"><Button size="sm" variant={tab === "host" ? "secondary" : "ghost"} onClick={() => setTab("host")}>Host</Button><Button size="sm" variant={tab === "kubernetes_cluster" ? "secondary" : "ghost"} onClick={() => setTab("kubernetes_cluster")}>Kubernetes</Button></div><input className="argus-input" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索资源" /><div className="argus-dual-list__columns"><section><header><span>未授权 ({available.length})</span><span><Button size="sm" variant="ghost" onClick={() => move(selected[tab].available, true)} disabled={!selected[tab].available.length}>批量移动</Button><Button size="sm" variant="ghost" onClick={() => moveAll(true)} disabled={!available.some((item) => !item.inherited)}>全部移动</Button></span></header>{pageItems(available).map((item) => renderItem(item, "available"))}{pager(availablePageCount)}</section><section><header><span>已授权 ({granted.length})</span><span><Button size="sm" variant="ghost" onClick={() => move(selected[tab].authorized, false)} disabled={!selected[tab].authorized.length}>批量移除</Button><Button size="sm" variant="ghost" onClick={() => moveAll(false)} disabled={!granted.some((item) => !item.inherited)}>全部移除</Button></span></header>{pageItems(granted).map((item) => renderItem(item, "authorized"))}{pager(grantedPageCount)}</section></div></div>;
}
