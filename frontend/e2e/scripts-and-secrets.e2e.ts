import { expect, test } from '@playwright/test';
import { createRequest, expectSettled, openApp, openRequest, STUB } from './support/app';

const SECRET = 'e2e-sk-9d41f0';

test("a script's results show in the inspector, with the secret masked", async ({ page }) => {
  await openApp(page);
  const id = await createRequest(page, `${STUB}/echo`, 'Authorization: Bearer {{TOKEN}}');

  const environmentId = await page.evaluate(async ({ id, secret }) => {
    const { commands } = window.snaprq;
    const [project] = await commands.execute<{ id: number }[]>('project.list', {});
    const env = await commands.execute<{ id: number }>('environment.create', { project_id: project.id, name: 'e2e' });
    const token = await commands.execute<{ id: number }>('variable.create', {
      environment_id: env.id, key: 'TOKEN', secret: true,
    });
    await commands.execute('variable.update', { id: token.id, key: 'TOKEN', value: secret });

    await commands.execute('script.attach', { request_id: id, project_id: project.id, script_name: 'e2e checks' });
    const scripts = await commands.execute<{ id: number; name: string }[]>('script.list', { project_id: project.id });
    const script = scripts.find((s) => s.name === 'e2e checks')!;
    await commands.execute('script.set_content', {
      id: script.id,
      content: `function afterResponse(res, ctx) {
  console.log("token is " + ctx.env.get("TOKEN"));
  ctx.test("server got the token", () =>
    ctx.expect(res.json().headers.authorization).toBe("Bearer " + ctx.env.get("TOKEN")));
  ctx.test("is a teapot", () => ctx.expect(res.status).toBe(418));
}`,
    });
    return env.id;
  }, { id, secret: SECRET });

  await openRequest(page, id);
  await page.evaluate(async ({ id, environmentId }) => {
    const { requests } = window.snaprq;
    await requests.send(await requests.getFull(id), [], environmentId);
  }, { id, environmentId });
  await expectSettled(page, id);

  const scripts = page.getByLabel('Inspector').locator('app-script-log');
  await expect(scripts.locator('.script-log-summary')).toHaveText(/1 passed\s*1 failed/);
  const header = scripts.locator('.script-log-header');
  if ((await header.getAttribute('aria-expanded')) !== 'true') await header.click();
  await expect(header).toHaveAttribute('aria-expanded', 'true');

  await expect(scripts.locator('.script-test').filter({ hasText: 'server got the token' })).not.toHaveClass(/is-failed/);
  await expect(scripts.locator('.script-test').filter({ hasText: 'is a teapot' })).toHaveClass(/is-failed/);
  await expect(scripts.locator('.script-console')).toHaveText('token is •••••');
  await expect(scripts).not.toContainText(SECRET);
});
