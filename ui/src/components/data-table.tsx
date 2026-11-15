import { createColumnHelper, tableFeatures, useTable } from "@tanstack/react-table";
import type { ColumnDef, RowData } from "@tanstack/react-table";

import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

export const dataTableFeatures = tableFeatures({});

export function createDataTableColumnHelper<TData extends RowData>() {
  return createColumnHelper<typeof dataTableFeatures, TData>();
}

type DataTableProps<TData extends RowData> = {
  // Keep columns and data stable between renders (module scope or memoized).
  columns: readonly ColumnDef<typeof dataTableFeatures, TData, any>[];
  data: TData[];
  loading?: boolean;
  emptyMessage: string;
};

const skeletonRows = 3;

export function DataTable<TData extends RowData>({
  columns,
  data,
  loading = false,
  emptyMessage,
}: DataTableProps<TData>) {
  const table = useTable({ features: dataTableFeatures, columns, data });
  const rows = table.getRowModel().rows;
  const columnCount = table.getAllLeafColumns().length;

  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          {table.getHeaderGroups().map((group) => (
            <TableRow key={group.id}>
              {group.headers.map((header) => (
                <TableHead key={header.id}>
                  {header.isPlaceholder ? null : <table.FlexRender header={header} />}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {loading &&
            Array.from({ length: skeletonRows }, (_, rowIndex) => (
              <TableRow key={rowIndex}>
                {Array.from({ length: columnCount }, (_, cellIndex) => (
                  <TableCell key={cellIndex}>
                    <Skeleton className="h-4 w-full max-w-40" />
                  </TableCell>
                ))}
              </TableRow>
            ))}
          {!loading && rows.length === 0 && (
            <TableRow>
              <TableCell colSpan={columnCount} className="h-24 text-center text-muted-foreground">
                {emptyMessage}
              </TableCell>
            </TableRow>
          )}
          {!loading &&
            rows.map((row) => (
              <TableRow key={row.id}>
                {row.getAllCells().map((cell) => (
                  <TableCell key={cell.id}>
                    <table.FlexRender cell={cell} />
                  </TableCell>
                ))}
              </TableRow>
            ))}
        </TableBody>
      </Table>
    </div>
  );
}
