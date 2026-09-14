import { useEffect, useState } from "react";
import { AlertTriangle, Check, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/sheet";
import {
  Drawer,
  DrawerClose,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
  DrawerDescription,
} from "@/components/ui/drawer";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
export function Decision({
  open,
  onOpenChange,
  onSaved,
  onAsk,
  company,
  choice,
  onChoice,
}: {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  onSaved: (value: string) => void;
  onAsk: () => void;
  company: string;
  choice: string;
  onChoice: (value: string) => void;
}) {
  const [mobile, setMobile] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const query = matchMedia("(max-width: 767px)");
    const update = () => setMobile(query.matches);
    update();
    query.addEventListener("change", update);
    return () => query.removeEventListener("change", update);
  }, []);
  function save() {
    try {
      localStorage.setItem(`books.demo.plan.${company}`, choice);
      onSaved(choice);
      onOpenChange(false);
    } catch {
      setError(
        "This browser could not save the plan. Please allow local storage and try again.",
      );
    }
  }
  const title = "When should we plan the repair?";
  const description = "Compare a $1,200 repair against your checking buffer.";
  const content = (
    <div className="space-y-6 p-6 pt-2">
      <RadioGroup
        value={choice}
        onValueChange={onChoice}
        aria-label="Repair timing"
      >
        {[
          { id: "now", title: "This week", balance: "$320" },
          { id: "later", title: "After payday", balance: "$1,520" },
        ].map((option) => (
          <label
            key={option.id}
            htmlFor={option.id}
            className={`flex cursor-pointer items-start gap-3 rounded-xl border p-4 ${choice === option.id ? "border-zinc-900 bg-zinc-50" : ""}`}
          >
            <RadioGroupItem id={option.id} value={option.id} className="mt-1" />
            <span>
              <strong className="text-sm">{option.title}</strong>
              <span className="mt-1 block text-sm text-muted-foreground">
                Lowest projected checking balance: {option.balance}
              </span>
              {option.id === "now" && (
                <span className="mt-3 flex items-center gap-2 rounded-md bg-amber-50 p-2 text-xs text-amber-900">
                  <AlertTriangle className="size-4" />
                  Below your $500 buffer
                </span>
              )}
            </span>
          </label>
        ))}
      </RadioGroup>
      <p className="text-sm text-muted-foreground">
        Assumes your next paycheck arrives Sep 20. These are illustrative demo
        scenarios.
      </p>
      {error && (
        <p role="alert" className="text-sm text-red-700">
          {error}
        </p>
      )}
      <div className="space-y-2">
        <Button className="h-11 w-full" onClick={save}>
          <Check />
          Save demo plan
        </Button>
        <Button
          variant="outline"
          className="h-11 w-full"
          onClick={() => {
            onOpenChange(false);
            onAsk();
          }}
        >
          Ask Books
        </Button>
      </div>
      <p className="text-center text-xs text-muted-foreground">
        Saved in this browser only. No money moves.
      </p>
    </div>
  );
  return mobile ? (
    <Drawer open={open} onOpenChange={onOpenChange}>
      <DrawerContent className="overflow-y-auto">
        <DrawerClose asChild>
          <Button
            variant="ghost"
            size="icon"
            className="absolute right-2 top-2 size-11"
            aria-label="Close decision"
          >
            <X />
          </Button>
        </DrawerClose>
        <DrawerHeader className="px-6 pt-8 text-left">
          <DrawerTitle>{title}</DrawerTitle>
          <DrawerDescription>{description}</DrawerDescription>
        </DrawerHeader>
        {content}
      </DrawerContent>
    </Drawer>
  ) : (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="overflow-y-auto sm:max-w-lg">
        <SheetHeader className="p-6 pt-16">
          <SheetTitle className="text-2xl tracking-tight">{title}</SheetTitle>
          <SheetDescription>{description}</SheetDescription>
        </SheetHeader>
        {content}
      </SheetContent>
    </Sheet>
  );
}
