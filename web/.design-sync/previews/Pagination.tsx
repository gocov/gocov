import { Pagination } from "gocov-web";

export function FirstPage() {
  return <Pagination older={{ to: "/" }} />;
}

export function InTheMiddle() {
  return <Pagination newer={{ to: "/" }} older={{ to: "/" }} />;
}

export function LastPage() {
  return <Pagination newer={{ to: "/" }} older={{ disabled: true }} />;
}

export function ASinglePage() {
  return <Pagination />;
}
