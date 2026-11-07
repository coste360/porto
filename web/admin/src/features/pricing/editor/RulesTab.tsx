import { CopyIcon, PlusIcon, Trash2Icon } from 'lucide-react';

import type { Item, PriceListIssue, PriceRule } from '@porto/shared/api';
import { useT } from '@porto/shared/i18n';
import { Badge } from '@porto/shared/ui/badge';
import { Button } from '@porto/shared/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@porto/shared/ui/card';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@porto/shared/ui/table';

import { STAGES } from '../lib/enums';
import { ruleDetail, scopeSummary } from '../lib/labels';
import { duplicateRule, newRule, refOf, removeRule, rulesOfStage, type EditorDoc } from './model';

export type RulesTabProps = {
  doc: EditorDoc;
  onChange: (doc: EditorDoc) => void;
  readOnly: boolean;
  items: Item[];
  issues: PriceListIssue[];
  onOpen: (rule: PriceRule) => void;
};

/** Tab Regole: rules grouped by stage; a click opens the rule Sheet. */
export function RulesTab({ doc, onChange, readOnly, items, issues, onOpen }: RulesTabProps) {
  const { t, i18n } = useT();
  const issuesByRule = new Map<string, number>();
  for (const i of issues) {
    if (i.ref.kind === 'rule' && i.ref.id)
      issuesByRule.set(i.ref.id, (issuesByRule.get(i.ref.id) ?? 0) + 1);
  }
  return (
    <div className="flex flex-col gap-6">
      {STAGES.map((stage) => {
        const rules = rulesOfStage(doc.rules, stage);
        return (
          <Card key={stage} data-testid={`rules-${stage}`}>
            <CardHeader className="flex flex-row items-center justify-between gap-2">
              <CardTitle>
                {t(`pricingEnums:stages.${stage}`)}{' '}
                <span className="text-muted-foreground text-sm font-normal">({rules.length})</span>
              </CardTitle>
              {readOnly ? null : (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => onOpen(newRule(stage, t(`pricing:rules.newName.${stage}`)))}
                >
                  <PlusIcon />
                  {t(`pricing:rules.add.${stage}`)}
                </Button>
              )}
            </CardHeader>
            <CardContent>
              <div className="overflow-x-auto rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('pricing:rules.columns.name')}</TableHead>
                      <TableHead>{t('pricing:rules.columns.detail')}</TableHead>
                      <TableHead>{t('pricing:rules.columns.scope')}</TableHead>
                      <TableHead>{t('pricing:rules.columns.priority')}</TableHead>
                      <TableHead>
                        <span className="sr-only">{t('common:actions.actions')}</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rules.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={5} className="text-muted-foreground h-14 text-center">
                          {t('pricing:rules.empty')}
                        </TableCell>
                      </TableRow>
                    ) : (
                      rules.map((r) => {
                        const ref = refOf(r);
                        const n = issuesByRule.get(ref) ?? 0;
                        return (
                          <TableRow key={ref} id={`pl-rule-${ref}`}>
                            <TableCell>
                              <span className="flex flex-wrap items-center gap-2">
                                <button
                                  type="button"
                                  className="text-left font-medium underline-offset-4 hover:underline"
                                  onClick={() => onOpen(r)}
                                >
                                  {r.name}
                                </button>
                                {r.active === false ? (
                                  <Badge variant="secondary">{t('common:status.inactive')}</Badge>
                                ) : null}
                                {r.stackable && stage !== 'base' ? (
                                  <Badge variant="outline">{t('pricing:rules.stackable')}</Badge>
                                ) : null}
                                {n > 0 ? (
                                  <Badge variant="destructive">
                                    {t('pricing:rules.issueCount', { count: n })}
                                  </Badge>
                                ) : null}
                              </span>
                            </TableCell>
                            <TableCell className="text-sm">
                              {ruleDetail(t, r, items, i18n.language)}
                            </TableCell>
                            <TableCell className="text-muted-foreground text-sm">
                              {scopeSummary(t, r.scope)}
                            </TableCell>
                            <TableCell>{r.priority ?? 0}</TableCell>
                            <TableCell>
                              {readOnly ? null : (
                                <div className="flex justify-end gap-1">
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="icon"
                                    aria-label={t('pricing:rules.duplicate', { name: r.name })}
                                    onClick={() =>
                                      onChange({
                                        ...doc,
                                        rules: [
                                          ...doc.rules,
                                          duplicateRule(r, t('pricing:rules.copySuffix')),
                                        ],
                                      })
                                    }
                                  >
                                    <CopyIcon />
                                  </Button>
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="icon"
                                    aria-label={t('pricing:rules.remove', { name: r.name })}
                                    onClick={() => onChange(removeRule(doc, r))}
                                  >
                                    <Trash2Icon />
                                  </Button>
                                </div>
                              )}
                            </TableCell>
                          </TableRow>
                        );
                      })
                    )}
                  </TableBody>
                </Table>
              </div>
            </CardContent>
          </Card>
        );
      })}
    </div>
  );
}
