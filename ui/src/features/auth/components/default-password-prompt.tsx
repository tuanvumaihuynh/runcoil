import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";

type DefaultPasswordPromptProps = {
  open: boolean;
  onChangePassword: () => void;
  onSkip: () => void;
};

export function DefaultPasswordPrompt({
  open,
  onChangePassword,
  onSkip,
}: DefaultPasswordPromptProps) {
  return (
    <AlertDialog open={open}>
      <AlertDialogContent onEscapeKeyDown={onSkip}>
        <AlertDialogHeader>
          <AlertDialogTitle>This account uses the default password</AlertDialogTitle>
          <AlertDialogDescription>Change it to keep the account safe.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onSkip}>Skip</AlertDialogCancel>
          <AlertDialogAction onClick={onChangePassword}>Change password</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
