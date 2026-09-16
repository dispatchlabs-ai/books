import { type ReactNode } from "react";
import { useSortable } from "@dnd-kit/react/sortable";
import {
  GripVertical,
  MoreHorizontal,
  EyeOff,
  ArrowUp,
  ArrowDown,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { TableRow, TableCell } from "@/components/ui/table";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu";

export function CashAccountRow({
  code,
  name,
  reserved,
  index,
  count,
  disabled,
  shortfall,
  onHide,
  onMove,
  children,
}: {
  code: string;
  name: string;
  reserved?: boolean;
  index: number;
  count: number;
  disabled: boolean;
  shortfall: boolean;
  onHide: () => void;
  onMove: (delta: number) => void;
  children: ReactNode;
}) {
  const { ref, handleRef, isDragSource } = useSortable({
    id: code,
    index,
    disabled,
  });
  return (
    <TableRow
      ref={ref}
      data-shortfall={shortfall}
      data-dragging={isDragSource || undefined}
      data-testid={`cash-row-${code}`}
    >
      <TableCell className="cash-account" data-label="Account">
        <div className="cash-account-content">
          <Button
            ref={handleRef}
            variant="ghost"
            size="icon-sm"
            className="cash-drag-handle"
            disabled={disabled}
            aria-label={`Reorder ${name}`}
          >
            <GripVertical aria-hidden="true" />
          </Button>
          <div className="cash-account-label">
            <span className="account-name">{name}</span>
            {reserved && <span className="account-meta">Reserve</span>}
          </div>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                className="cash-row-menu"
                disabled={disabled}
                aria-label={`Account options for ${name}`}
              >
                <MoreHorizontal aria-hidden="true" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-44">
              <DropdownMenuItem
                disabled={index === 0}
                onSelect={() => onMove(-1)}
              >
                <ArrowUp />
                Move up
              </DropdownMenuItem>
              <DropdownMenuItem
                disabled={index === count - 1}
                onSelect={() => onMove(1)}
              >
                <ArrowDown />
                Move down
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={onHide}>
                <EyeOff />
                Hide account
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </TableCell>
      {children}
    </TableRow>
  );
}
